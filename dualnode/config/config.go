package config

type Config struct {
	Chains []ChainConfig
	API    string
}

type ChainConfig struct {
	Type     string `json:"type"`
	ChainID  string `json:"chain_id"`
	Endpoint string `json:"endpoint"`
}
