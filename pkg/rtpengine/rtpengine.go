package rtpengine

// webrtc\src\webrtc\api\peerconnectioninterface.h
//
// disable_encryption = true 取消SRTP

// 关闭srtp思想利用webrtc组件发送rtp包。
//开启SRTP发包函数调用栈
//P2PTransportChannel::SendPacket
//DtlsTransport::SendPacket
//RtpTransport::SendPacket
//RtpTransport::SendRtpPacket
//-----SrtpTransport::SendPacket //关闭时没有
//-----SrtpTransport::SendRtpPacket //关闭时没有
//BaseChannel::SendPacket
//BaseChannel::OnMessage
//VoiceChannel::OnMessage
//MessageQueue::Dispatch
//Thread::ProcessMessages
//Thread::Run
//Thread::PreRun
// 参考 webRTC samples： rtp-forward.
// 媒体就只有rtp，不考虑单独实现.
