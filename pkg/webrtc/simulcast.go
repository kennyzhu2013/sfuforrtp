package webrtc

// simulcast: 指同时发送同一视频的不同清晰度的多路视频流(多方通话或会议时用到).
// simulcast模型:
// SDK---SFU--->WebRTCReceiver(audio).buffer[0].ReadExtended---->downTracks[0][0].WriteRTP->SDK
//       |                                              |....
//       |                                              |--->downTracks[0][N].WriteRTP
//       |
//       |---->WebRTCReceiver(video).buffer[0].ReadExtended---->downTracks[0][0].WriteRTP
//                    |                                    |....
//                    |                                    |---->downTracks[0][N].WriteRTP
//                    |
//                    |------------->buffer[1].ReadExtended---->downTracks[1][0].WriteRTP
//                    |                                     |....
//                    |                                     |----->downTracks[1][N].WriteRTP
//                    |
//                    |------------->buffer[2].ReadExtended---->downTracks[2][0].WriteRTP
//                                                          |....
//                                                          |------>downTracks[2][N].WriteRTP
