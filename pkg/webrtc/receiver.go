package webrtc

import (
	"common/gpool"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
	"io"
	"math/rand"
	"mediasfu/pkg/webrtc/buffer"
	"sync"
	"sync/atomic"
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
	DeleteDownTrack(layer int, id string)
	OnCloseHandler(fn func())
	SendRTCP(p []rtcp.Packet)
	SetRTCPCh(ch chan []rtcp.Packet)
	GetSenderReportTime(layer int) (rtpTS uint32, ntpTS uint64)
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
	return &WebRTCReceiver{
		peerID:      pid,
		receiver:    receiver,
		trackID:     track.ID(),
		streamID:    track.StreamID(),
		codec:       track.Codec(),
		kind:        track.Kind(),
		nackWorker:  gpool.NewPool(1),
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
func (w *WebRTCReceiver) AddUpTrack(track *webrtc.TrackRemote, buff *buffer.Buffer, bestQualityFirst bool) {
	if w.closed.get() {
		return
	}



	w.Lock()
	w.upTracks = track // 一个上流uptrack(Publisher.).
	w.buffers = buff
	w.downTracks.Store(make([]*DownTrack, 0, 10)) // 最大十个DownTrack（其他的）
	w.Unlock()


	go w.writeRTP()
}


// send publish buff to all downTracks.
// 核心函数.
func (w *WebRTCReceiver) writeRTP() {
	defer func() {
		w.closeOnce.Do(func() {
			w.closed.set(true)
			w.closeTracks()
		})
	}()

	pli := []rtcp.Packet{
		&rtcp.PictureLossIndication{SenderSSRC: rand.Uint32(), MediaSSRC: w.SSRC()}, // layer
	}

	for {
		// 兼容扩展包.
		pkt, err := w.buffers[layer].ReadExtended()
		if err == io.EOF {
			return
		}

		// Simulcast扩展报twcc处理.
		if w.isSimulcast {
			if w.pending[layer].get() { //
				if pkt.KeyFrame {
					w.Lock()
					for idx, dt := range w.pendingTracks[layer] {
						w.deleteDownTrack(dt.CurrentSpatialLayer(), dt.peerID)
						w.storeDownTrack(layer, dt)
						dt.SwitchSpatialLayerDone(int32(layer))
						w.pendingTracks[layer][idx] = nil
					}
					w.pendingTracks[layer] = w.pendingTracks[layer][:0]
					w.pending[layer].set(false)
					w.Unlock()
				} else { // 当前帧不是关键帧然后发送PLI(Picture Loss Indication，关键帧请求）
					// 发送方接收到接收方反馈的PLI或SLI需要重新让编码器生成关键帧并发送给接收端.（最好做成定时的而不是Simulcast开启情况下每次都请求发关键帧）
					// TODO:待优化.
					w.SendRTCP(pli) // w.SendRTCP(pli) // 发生SwitchDownTrack.
				}
			}
		}

		// client subscriber.
		for _, dt := range w.downTracks[layer].Load().([]*DownTrack) {
			if err = dt.WriteRTP(pkt, layer); err != nil {
				if err == io.EOF && err == io.ErrClosedPipe {
					w.Lock()
					w.deleteDownTrack(layer, dt.id)
					w.Unlock()
				}
				log.Error().Err(err).Str("id", dt.id).Msg("Error writing to down track")
			}
		}
	}

}

// closeTracks close all tracks from Receiver
func (w *WebRTCReceiver) closeTracks() {
	for idx, a := range w.available {
		if !a.get() {
			continue
		}
		for _, dt := range w.downTracks[idx].Load().([]*DownTrack) {
			dt.Close()
		}
	}
	w.nackWorker.StopWait()
	if w.onCloseHandler != nil {
		w.onCloseHandler()
	}
}

func (w *WebRTCReceiver) downTrackSubscribed(layer int, dt *DownTrack) bool {
	dts := w.downTracks[layer].Load().([]*DownTrack)
	for _, cdt := range dts {
		if cdt == dt {
			return true
		}
	}
	return false
}

func (w *WebRTCReceiver) storeDownTrack(layer int, dt *DownTrack) {
	dts := w.downTracks[layer].Load().([]*DownTrack)
	ndts := make([]*DownTrack, len(dts)+1)
	copy(ndts, dts)
	ndts[len(ndts)-1] = dt
	w.downTracks[layer].Store(ndts)
}
