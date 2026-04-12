package elasticsearch

import (
	"time"
)

type Config struct {
	Address               string        `env:"ADDRESS"                 json:"address"`
	Username              string        `env:"USERNAME"                json:"username"`
	Password              string        `env:"PASSWORD"                json:"password"`
	CACert                []byte        `env:"CA_CERT"                 json:"caCert"`
	IndexOperationTimeout time.Duration `env:"INDEX_OPERATION_TIMEOUT" json:"indexOperationTimeout"`
}
