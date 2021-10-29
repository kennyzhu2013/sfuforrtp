package webrtc


// Session represents a set of peers. Transports inside a SessionLocal
// are automatically subscribed to each other.
// 双PC模式：上下行流都用同一个PC重协商，浏览器兼容性差，造成SFU状态竞争冲突问题等。
// 网络流不需要考虑浏览器兼容性.
type Session interface {
	ID() string

	//
	Publish(router Router, r Receiver) // 把Receiver的流发布到router中，给Session中的每个Peer增加一个AddDownTracks. // AddDownTracks
	Subscribe(peer Peer)  // 把peer的Subscriber订阅到房间中其他peer.
	AddPeer(peer Peer)   // 房间增加一个peer
	GetPeer(peerID string) Peer // 获取peer,一个bill-id公用一个
	RemovePeer(peer Peer) //获取声音检测
	AudioObserver() *AudioObserver // 可选.

	Peers() []Peer // 默认两个.
}
	// not use data channel for rtp session.
	//// AddDatachannel(owner string, dc *webrtc.DataChannel)
	//// GetDCMiddlewares() []*Datachannel
	//GetFanOutDataChannelLabels() []string
	//GetDataChannels(peerID, label string) (dcs []*webrtc.DataChannel)
	//FanOutMessage(origin, label string, msg webrtc.DataChannelMessage)


// 一个会话管理多个peer.
