package webrtc

// rtp无绑定发送接口.
const (
	// for turn listen.
	turnMinPort = 32768
	turnMaxPort = 46883

	// relay to sfu.
	sfuMinPort  = 46884
	sfuMaxPort  = 60999
)

// not use https yet.

// WebRTCConfig defines parameters for ice
type TurnConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	Realm     string   `mapstructure:"realm"`
	Address   string   `mapstructure:"address"`
	Cert      string   `mapstructure:"cert"`
	Key       string   `mapstructure:"key"`
	// Auth      TurnAuth `mapstructure:"auth"`
	PortRange []uint16 `mapstructure:"portrange"`
}


// TODO:是否需要采用dtls协议实现turnserver...
// 	直接udp rtp turn？
// 信令在等待内网udp hello后不用turn server：内网handshake.