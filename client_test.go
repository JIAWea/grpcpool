package grpcpool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractName(t *testing.T) {
	s := &ServiceClientPool{}
	name := s.ExtractServiceName("/account/Register")

	assert.Equal(t, name, "/account")
}

func TestHostServiceNames(t *testing.T) {
	m := NewTargetServiceNames()
	m.Set("127.0.0.1:8080", "/account", "/order")
	assert.Equal(t, m.List()["127.0.0.1:8080"][0], "/account")
	assert.Equal(t, m.List()["127.0.0.1:8080"][1], "/order")
}
