package webrtc

import (
	"common/gpool"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"io"
	"mediasfu/pkg/webrtc/buffer"
	"sync"
	"sync/atomic"
	"time"
)

// Receiver defines a interface for a track receivers
// TODO:支持视频情况下拥塞控制.
type Receiver interface {
	TrackID() string
	StreamID() string
	Codec() webrtc.RTPCodecParameters
	Kind() webrtc.RTPCodecType
	SSRC() uint32
	SetTrackMeta(trackID, streamID string)
	AddUpTrack(track *webrtc.TrackRemote, buffer *buffer.Buffer)
	AddDownTrack(track *DownTrack)
	GetBitrate() uint64
	RetransmitPackets(track *DownTrack, packets []packetMeta) error
	DeleteDownTrack(id string)
	OnCloseHandler(fn func())
	SendRTCP(p []rtcp.Packet)
	SetRTCPCh(ch chan []rtcp.Packet)
	GetSenderReportTime() (rtpTS uint32, ntpTS uint64)
}

// WebRTCReceiver receives a video track
// Receiver从客户端接收rtp流，并发送rtcp
// 基于单纯rtp流实现:
// 不考虑统计.
// 不考虑srtp.
type WebRTCReceiver struct {
	sync.Mutex
	closeOnce sync.Once

	peerID         string
	trackID        string
	streamID       string
	kind           webrtc.RTPCodecType
	closed         atomicBool
	bandwidth      uint64
	lastPli        int64
	stream         string
	receiver       *webrtc.RTPReceiver
	codec          webrtc.RTPCodecParameters
	rtcpCh         chan []rtcp.Packet // 支持rtcp

	buffers        *buffer.Buffer
	upTracks       *webrtc.TrackRemote

	// 不考虑simulcast.
	// isSimulcast    bool // always false
	downTracks     atomic.Value // []*DownTrack // A Value provides an atomic load and store of a consistently typed value.
	nackWorker     *gpool.Pool // 用自身实现的协程池.

	onCloseHandler func()
}

// NewWebRTCReceiver creates a new webrtc track receivers, track 用来跟踪媒体.
func NewWebRTCReceiver(receiver *webrtc.RTPReceiver, track *webrtc.TrackRemote, pid string) Receiver {
	worker,_ := gpool.NewPool(1)
	return &WebRTCReceiver{
		peerID:      pid,
		receiver:    receiver,
		trackID:     track.ID(),
		streamID:    track.StreamID(),
		codec:       track.Codec(),
		kind:        track.Kind(),
		nackWorker:  worker,
	}
}

func (w *WebRTCReceiver) SetTrackMeta(trackID, streamID string) {
	w.streamID = streamID
	w.trackID = trackID
}

func (w *WebRTCReceiver) StreamID() string {
	return w.streamID
}

func (w *WebRTCReceiver) TrackID() string {
	return w.trackID
}

func (w *WebRTCReceiver) SSRC() uint32 {
	if w.upTracks != nil {
		return uint32(w.upTracks.SSRC())
	}
	return 0
}

func (w *WebRTCReceiver) Codec() webrtc.RTPCodecParameters {
	return w.codec
}

func (w *WebRTCReceiver) Kind() webrtc.RTPCodecType {
	return w.kind
}


// 有TrackLocal表示表示本地发往远端的track，对应的自然也会有TrackRemote表示远端发到本地的track：
// 转发RTP包的核心函数.
func (w *WebRTCReceiver) AddUpTrack(track *webrtc.TrackRemote, buff *buffer.Buffer) {
	if w.closed.get() {
		return
	}

	w.Lock()
	w.upTracks = track // 一个上流行uptrack(Publisher.)，远端发往本地的.
	w.buffers = buff
	w.downTracks.Store(make([]*DownTrack, 0, 10)) // 最大十个DownTrack（其他的）
	w.Unlock()

	go w.writeRTP()
}


func (w *WebRTCReceiver) AddDownTrack(track *DownTrack) {
	if w.closed.get() {
		return
	}

	if w.downTrackSubscribed(track) {
		return
	}
	// track.SetInitialLayers(0, 0)
	// track.trackType = SimpleDownTrack
	w.Lock()
	w.storeDownTrack(track)
	w.Unlock()
}

func (w *WebRTCReceiver) GetBitrate() uint64 {
	return w.buffers.Bitrate()
}

func (w *WebRTCReceiver) GetMaxTemporalLayer() int32 {
	return w.buffers.MaxTemporalLayer()
}

// OnCloseHandler method to be called on remote tracked removed
func (w *WebRTCReceiver) OnCloseHandler(fn func()) {
	w.onCloseHandler = fn
}

// send publish buff to all downTracks.
// 核心函数.
// 放音功能在原来的xmedia实现.
// 接通后放音. Local,
func (w *WebRTCReceiver) writeRTP() {
	defer func() {
		w.closeOnce.Do(func() {
			w.closed.set(true)
			w.closeTracks()
		})
	}()

	// pli
	//pli := []rtcp.Packet{
	//	&rtcp.PictureLossIndication{SenderSSRC: rand.Uint32(), MediaSSRC: w.SSRC()}, // layer
	//}
	for {
		// 兼容扩展包.
		// 放音如何处理.
		pkt, err := w.buffers.ReadExtended()
		if err == io.EOF {
			return
		}

		// Simulcast扩展报twcc处理.
		// 不主动发送pli.
		// client subscriber.
		for _, dt := range w.downTracks.Load().([]*DownTrack) {
			if err = dt.WriteRTP(pkt); err != nil {
				if err == io.EOF && err == io.ErrClosedPipe {
					w.Lock()
					w.deleteDownTrack(dt.id)
					w.Unlock()
				}
				Logger.Error(err.Error() + "id" + dt.id + "Error writing to down track")
			}
		}
	}
}

// closeTracks close all tracks from Receiver
func (w *WebRTCReceiver) closeTracks() {
	for _, dt := range w.downTracks.Load().([]*DownTrack) {
		dt.Close()
	}

	w.nackWorker.Release()
	if w.onCloseHandler != nil {
		w.onCloseHandler()
	}
}

func (w *WebRTCReceiver) downTrackSubscribed(dt *DownTrack) bool {
	dts := w.downTracks.Load().([]*DownTrack)
	for _, cdt := range dts {
		if cdt == dt {
			return true
		}
	}
	return false
}

func (w *WebRTCReceiver) storeDownTrack(dt *DownTrack) {
	dts := w.downTracks.Load().([]*DownTrack)
	ndts := make([]*DownTrack, len(dts)+1)
	copy(ndts, dts)
	ndts[len(ndts)-1] = dt
	w.downTracks.Store(ndts)
}


// DeleteDownTrack removes a DownTrack from a Receiver
func (w *WebRTCReceiver) DeleteDownTrack(id string) {
	if w.closed.get() {
		return
	}
	w.Lock()
	w.deleteDownTrack(id)
	w.Unlock()
}

func (w *WebRTCReceiver) deleteDownTrack(id string) {
	dts := w.downTracks.Load().([]*DownTrack)
	ndts := make([]*DownTrack, 0, len(dts))
	for _, dt := range dts {
		if dt.id != id {
			ndts = append(ndts, dt)
		}
	}
	w.downTracks.Store(ndts)
}

func (w *WebRTCReceiver) SendRTCP(p []rtcp.Packet) {
	if _, ok := p[0].(*rtcp.PictureLossIndication); ok {
		if time.Now().UnixNano()-atomic.LoadInt64(&w.lastPli) < 500e6 {
			return
		}
		atomic.StoreInt64(&w.lastPli, time.Now().UnixNano())
	}

	w.rtcpCh <- p
}

func (w *WebRTCReceiver) SetRTCPCh(ch chan []rtcp.Packet) {
	w.rtcpCh = ch
}

func (w *WebRTCReceiver) GetSenderReportTime() (rtpTS uint32, ntpTS uint64) {
	rtpTS, ntpTS, _ = w.buffers.GetSenderReportData()
	return
}

// not need to wait done.
func (w *WebRTCReceiver) RetransmitPackets(track *DownTrack, packets []packetMeta) error {
	if w.nackWorker.IsClosed() {
		return io.ErrClosedPipe
	}

	_ = w.nackWorker.Submit(func() {
		src := packetFactory.Get().(*[]byte)
		for _, meta := range packets {
			pktBuff := *src
			buff := w.buffers
			if buff == nil {
				break
			}
			i, err := buff.GetPacket(pktBuff, meta.sourceSeqNo)
			if err != nil {
				if err == io.EOF {
					break
				}
				continue
			}
			var pkt rtp.Packet
			if err = pkt.Unmarshal(pktBuff[:i]); err != nil {
				continue
			}
			pkt.Header.SequenceNumber = meta.targetSeqNo
			pkt.Header.Timestamp = meta.timestamp
			pkt.Header.SSRC = track.ssrc
			pkt.Header.PayloadType = track.payloadType
			// simulcast ignored.

			if _, err = track.writeStream.WriteRTP(&pkt.Header, pkt.Payload); err != nil {
				Logger.Error(err, "Writing rtx packet err")
			} else {
				track.UpdateStats(uint32(i))
			}
		}
		packetFactory.Put(src)
	})
	return nil
}