package elasticsearch

import (
	"testing"
	"time"

	"github.com/shoenig/test"
)

func TestConfig(T *testing.T) {
	T.Parallel()

	T.Run("zero value", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		test.EqOp(t, "", cfg.Address)
		test.EqOp(t, "", cfg.Username)
		test.EqOp(t, "", cfg.Password)
		test.Nil(t, cfg.CACert)
		test.EqOp(t, time.Duration(0), cfg.IndexOperationTimeout)
	})

	T.Run("with values", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Address:               "http://localhost:9200",
			Username:              "elastic",
			Password:              "password",
			CACert:                []byte("cert"),
			IndexOperationTimeout: 5 * time.Second,
		}

		test.EqOp(t, "http://localhost:9200", cfg.Address)
		test.EqOp(t, "elastic", cfg.Username)
		test.EqOp(t, "password", cfg.Password)
		test.Eq(t, []byte("cert"), cfg.CACert)
		test.EqOp(t, 5*time.Second, cfg.IndexOperationTimeout)
	})
}
