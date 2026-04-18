package proxy

import "net/http"

type ClientFactory struct {
	builder *TransportBuilder
}

func NewClientFactory(builder *TransportBuilder) *ClientFactory {
	return &ClientFactory{builder: builder}
}

func (f *ClientFactory) NewClient(proxyURL string) (*http.Client, error) {
	transport, err := f.builder.Build(proxyURL)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport}, nil
}
