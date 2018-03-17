package promout

import (
	"errors"
)

type config struct {
	Addr string `config:"addr"`
}

var (
	// 默认配置
	defaultConfig = config{
		Addr: ":9123",
	}
)

// 检查配置
func (c *config) Validate() error {
	if c.Addr == "" {
		return errors.New(" Addr can not be null")
	}
	return nil
}
