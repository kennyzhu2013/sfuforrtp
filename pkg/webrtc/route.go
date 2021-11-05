package webrtc

import (
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"mediasfu/pkg/webrtc/buffer"
	"sync"
)

// Router defines a track rtp/rtcp Router
type Router interface {
	ID() string
	AddReceiver(receiver *webrtc.RTPReceiver, track *webrtc.TrackRemote, trackID, streamID string) (Receiver, bool)
	AddDownTracks(s *Subscriber, r Receiver) error
	SetRTCPWriter(func([]rtcp.Packet) error)
	AddDownTrack(s *Subscriber, r Receiver) (*DownTrack, error)
	Stop()
}

// RouterConfig defines Router configurations
// https://pkg.go.dev/github.com/mitchellh/mapstructure
//1、该方法支持基本类型，不支持结构体 struct
//2、该方法支持map结构中的值是map的嵌套结构
type RouterConfig struct {
	// WithStats           bool            `mapstructure:"withstats"`
	MaxBandwidth        uint64          `mapstructure:"maxbandwidth"`
	MaxPacketTrack      int             `mapstructure:"maxpackettrack"`

	// for audio observer.
	AudioLevelInterval  int             `mapstructure:"audiolevelinterval"`
	AudioLevelThreshold uint8           `mapstructure:"audiolevelthreshold"`
	AudioLevelFilter    int             `mapstructure:"audiolevelfilter"`
}

// publish的订购关系实际路由.
// 订购关系复杂.
// 统计也在route里打点实现.
type router struct {
	sync.RWMutex
	id            string

	// 不支持twcc和统计.
	//twcc          *twcc.Responder
	//stats         map[uint32]*stats.Stream
	rtcpCh        chan []rtcp.Packet
	stopCh        chan struct{}
	config        RouterConfig
	session       Session
	receivers     map[string]Receiver // many receiver.
	bufferFactory *buffer.Factory
	writeRTCP     func([]rtcp.Packet) error
}

// newRouter for routing rtp/rtcp packets
func newRouter(id string, session Session, config *WebRTCTransportConfig) Router {
	ch := make(chan []rtcp.Packet, 10)
	r := &router{
		id:            id,
		rtcpCh:        ch,
		stopCh:        make(chan struct{}),
		config:        config.Router,
		session:       session,
		receivers:     make(map[string]Receiver),
		bufferFactory: config.BufferFactory,
	}

	return r
}

func (r *router) ID() string {
	return r.id
}

// TODO:基于事件替换channel.
func (r *router) Stop() {
	r.stopCh <- struct{}{}
}


// 设置rtp和rtcp的buffer的各种回调.
// RTPReceiver allows an application to inspect the receipt of a TrackRemote
//
// srtp和srtcp流向是这样的：
//客户端---srtp--->srtp.ReadStreamSRTP------->SFU
//客户端<---srtcp---srtp.ReadStreamSRTCP<------SFU
//当包到达pion/srtp时，就会触发ReadStreamSRTP.init函数和ReadStreamSRTCP.init函数：
//ReadStreamSRTP.init调用自定义的BufferFactory.GetOrNew函数了，new了一个buffer
//ReadStreamSRTCP.init调用自定义的BufferFactory.GetOrNew函数，new一个rtcpReader
func (r *router) AddReceiver(receiver *webrtc.RTPReceiver, track *webrtc.TrackRemote, trackID, streamID string) (Receiver, bool) {
	r.Lock()
	defer r.Unlock()

	publish := false

	// //这里获取了之前ReadStreamSRTP init函数中，new出来的buffer和rtcpReader，开始搞事情
	// bufferFactory继承自SettingEngine.BufferFactory(来自DTLSTransport的srtp的bufferFactory).
	buff, rtcpReader := r.bufferFactory.GetBufferPair(uint32(track.SSRC()))

	// //设置rtcp的回调，比如nack、twcc、rr
	buff.OnFeedback(func(fb []rtcp.Packet) {
		r.rtcpCh <- fb
	})

	//
	if track.Kind() == webrtc.RTPCodecTypeAudio {
		// 如果是音频track，设置OnAudioLevel回调声音控制回调.
		buff.OnAudioLevel(func(level uint8) {
			r.session.AudioObserver().observe(streamID, level)
		})
		r.session.AudioObserver().addStream(streamID)

	} else if track.Kind() == webrtc.RTPCodecTypeVideo {
		//if r.twcc == nil {
		//	// 如果是视频track，创建twcc计算器，并设置回调，当计算器生成twcc包就会回调.
		//	// 注：这是rtp扩展字段支持.
		//// //设置buffer的twcc回调，buffer收到包后调用，塞入twcc计算器
		////  server->client: twcc计算生成rtcp包，再回调OnFeedback发送给客户端.
	}

	// TODO: 每个track的统计.
	// 设置rtcpReader.OnPacket
	rtcpReader.OnPacket(func(bytes []byte) {
		// 收到SDES、SR包做些处理
		pkts, err := rtcp.Unmarshal(bytes)
		if err != nil {
			Logger.Error(err, "Unmarshal rtcp receiver packets err")
			return
		}
		for _, pkt := range pkts {
			switch pkt := pkt.(type) {
			case *rtcp.SourceDescription:
				// SName修改.
			case *rtcp.SenderReport:
				buff.SetSenderReportData(pkt.RTPTime, pkt.NTPTime)
				// 更新统计结果.
			}
		}
	})

	// 找出这个track要发给谁
	recv, ok := r.receivers[trackID]
	if !ok {
		//创建WebRTCReceiver并设置回调.
		recv = NewWebRTCReceiver(receiver, track, r.id)
		r.receivers[trackID] = recv
		recv.SetRTCPCh(r.rtcpCh)
		recv.OnCloseHandler(func() {
			// audio track need to remove observer.
			if recv.Kind() == webrtc.RTPCodecTypeAudio {
				r.session.AudioObserver().removeStream(track.StreamID())
			}
			r.deleteReceiver(trackID, uint32(track.SSRC()))
		})
		publish = true
	}

	// 把track buffer塞入recv
	// 创建uptrack.
	recv.AddUpTrack(track, buff)

	buff.Bind(receiver.GetParameters(), buffer.Options{
		MaxBitRate: r.config.MaxBandwidth,
	})

	return recv, publish
}

// for download track.
func (r *router) AddDownTracks(s *Subscriber, recv Receiver) error {
	r.Lock()
	defer r.Unlock()

	if s.noAutoSubscribe {
		Logger.Info("peer turns off automatic subscription, skip tracks add")
		return nil
	}

	// Publish: add recv receiver of download track to s.
	if recv != nil {
		if _, err := r.AddDownTrack(s, recv); err != nil {
			return err
		}
		s.negotiate()
		return nil
	}

	// Subscribe: add other receivers of download track to s.
	// download tracks
	if len(r.receivers) > 0 {
		for _, rcv := range r.receivers {
			if _, err := r.AddDownTrack(s, rcv); err != nil {
				return err
			}
		}
		s.negotiate()
	}
	return nil
}

func (r *router) SetRTCPWriter(fn func(packet []rtcp.Packet) error) {
	r.writeRTCP = fn
	go r.sendRTCP()
}

// sub----.Receivers
func (r *router) AddDownTrack(sub *Subscriber, recv Receiver) (*DownTrack, error) {
	for _, dt := range sub.GetDownTracks(recv.StreamID()) {
		if dt.ID() == recv.TrackID() {
			return dt, nil
		}
	}

	// 编解码取合集.
	codec := recv.Codec()
	if err := sub.me.RegisterCodec(codec, recv.Kind()); err != nil {
		return nil, err
	}

	// downTrack include recv sdp.
	downTrack, err := NewDownTrack(webrtc.RTPCodecCapability{
		MimeType:     codec.MimeType,
		ClockRate:    codec.ClockRate,
		Channels:     codec.Channels,
		SDPFmtpLine:  codec.SDPFmtpLine,
		RTCPFeedback: []webrtc.RTCPFeedback{{"goog-remb", ""}, {"nack", ""}, {"nack", "pli"}},
	}, recv, r.bufferFactory, sub.id, r.config.MaxPacketTrack)
	if err != nil {
		return nil, err
	}
	// Create webrtc sender for the peer we are sending track to
	if downTrack.transceiver, err = sub.pc.AddTransceiverFromTrack(downTrack, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	}); err != nil {
		return nil, err
	}

	// nolint:scopelint
	downTrack.OnCloseHandler(func() {
		if sub.pc.ConnectionState() != webrtc.PeerConnectionStateClosed {
			if err := sub.pc.RemoveTrack(downTrack.transceiver.Sender()); err != nil {
				if err == webrtc.ErrConnectionClosed {
					return
				}
				Logger.Error(err, "Error closing down track")
			} else {
				sub.RemoveDownTrack(recv.StreamID(), downTrack)
				sub.negotiate()
			}
		}
	})

	downTrack.OnBind(func() {
		go sub.sendStreamDownTracksReports(recv.StreamID())
	})

	sub.AddDownTrack(recv.StreamID(), downTrack)
	recv.AddDownTrack(downTrack)
	return downTrack, nil
}

func (r *router) deleteReceiver(track string, ssrc uint32) {
	r.Lock()
	delete(r.receivers, track)
	// delete(r.stats, ssrc)
	r.Unlock()
}

func (r *router) sendRTCP() {
	for {
		select {
		case pkts := <-r.rtcpCh:
			if err := r.writeRTCP(pkts); err != nil {
				Logger.Error(err, "Write rtcp to peer err", "peer_id", r.id)
			}
		case <-r.stopCh:
			return
		}
	}
}

// 基于rtcp的promethues统计.
//func (r *router) updateStats(stream *stats.Stream) {
//	calculateLatestMinMaxSenderNtpTime := func(cname string) (minPacketNtpTimeInMillisSinceSenderEpoch uint64, maxPacketNtpTimeInMillisSinceSenderEpoch uint64) {
//		if len(cname) < 1 {
//			return
//		}
//		r.RLock()
//		defer r.RUnlock()
//
//		for _, s := range r.stats {
//			if s.GetCName() != cname {
//				continue
//			}
//
//			clockRate := s.Buffer.GetClockRate()
//			srrtp, srntp, _ := s.Buffer.GetSenderReportData()
//			latestTimestamp, _ := s.Buffer.GetLatestTimestamp()
//
//			fastForwardTimestampInClockRate := fastForwardTimestampAmount(latestTimestamp, srrtp)
//			fastForwardTimestampInMillis := (fastForwardTimestampInClockRate * 1000) / clockRate
//			latestPacketNtpTimeInMillisSinceSenderEpoch := ntpToMillisSinceEpoch(srntp) + uint64(fastForwardTimestampInMillis)
//
//			if 0 == minPacketNtpTimeInMillisSinceSenderEpoch || latestPacketNtpTimeInMillisSinceSenderEpoch < minPacketNtpTimeInMillisSinceSenderEpoch {
//				minPacketNtpTimeInMillisSinceSenderEpoch = latestPacketNtpTimeInMillisSinceSenderEpoch
//			}
//			if 0 == maxPacketNtpTimeInMillisSinceSenderEpoch || latestPacketNtpTimeInMillisSinceSenderEpoch > maxPacketNtpTimeInMillisSinceSenderEpoch {
//				maxPacketNtpTimeInMillisSinceSenderEpoch = latestPacketNtpTimeInMillisSinceSenderEpoch
//			}
//		}
//		return minPacketNtpTimeInMillisSinceSenderEpoch, maxPacketNtpTimeInMillisSinceSenderEpoch
//	}
//
//	setDrift := func(cname string, driftInMillis uint64) {
//		if len(cname) < 1 {
//			return
//		}
//		r.RLock()
//		defer r.RUnlock()
//
//		for _, s := range r.stats {
//			if s.GetCName() != cname {
//				continue
//			}
//			s.SetDriftInMillis(driftInMillis)
//		}
//	}
//
//	cname := stream.GetCName()
//
//	minPacketNtpTimeInMillisSinceSenderEpoch, maxPacketNtpTimeInMillisSinceSenderEpoch := calculateLatestMinMaxSenderNtpTime(cname)
//
//	driftInMillis := maxPacketNtpTimeInMillisSinceSenderEpoch - minPacketNtpTimeInMillisSinceSenderEpoch
//
//	setDrift(cname, driftInMillis)
//
//	stream.CalcStats()
//}
