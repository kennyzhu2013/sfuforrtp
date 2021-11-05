package webrtc

import (
	log "common/log/newlog"
	"github.com/pion/webrtc/v3"
	"mediasfu/pkg/webrtc/buffer"
	"sync"
)

// sfu 核心功能转发:只关注媒体.
// RTP标准功能开发.

//├── audioobserver.go //声音检测
//├── datachannel.go //dc中间件的封装
//├── downtrack.go //下行track
//├── helpers.go //工具函数集
//├── mediaengine.go //SDP相关codec、rtp参数设置
//├── peer.go //peer封装，一个peer包含一个publisher和一个subscriber，双pc设计
//├── publisher.go //publisher，封装上行pc
//├── receiver.go //subscriber，封装下行pc
//├── router.go //router，包含pc、session、一组receivers，归publisher拥有.
//├── sequencer.go //记录包的信息：序列号sn、偏移、时间戳ts等
//├── session.go //会话，包含多个peer、dc
//├── sfu.go //分发单元，包含多个session、dc
//├── simulcast.go //大小流配置
//├── subscriber.go //subscriber，封装下行pc、DownTrack、dc
//└── turn.go //内置turn server，为webrtc交互的媒体流保持长连接(媒体connect后完成连接)。如果是固定rtp转接，直接无绑定udp发送即可..


// 直联方式.
// 本包只处理信令层.

// TODO:. 以后再实现.
// (1) Data Channel
// (2)
var (
	// Logger is an implementation of log.Logger. If is not provided - will be turned off.
	Logger log.Logger = log.GetLogger()
	packetFactory *sync.Pool // 使用pool
)

// webrtc传输配置
type WebRTCTransportConfig struct {
	Configuration webrtc.Configuration
	Setting       webrtc.SettingEngine
	Router        RouterConfig
	BufferFactory *buffer.Factory
}