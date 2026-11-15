package kubernetes

import (
	"io"
	"net/http"
	"strings"
)

// Bound only Deployment-list responses. Action/get/patch and unrelated API calls
// keep their existing transport behavior. Aggregate scan limits apply separately.
type deploymentListTransport struct{ next http.RoundTripper }

const deploymentPageBytes = 16 << 20

func (t deploymentListTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	if request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/deployments") {
		if response.ContentLength > deploymentPageBytes {
			response.Body.Close()
			return nil, scanFailure("limit_exceeded")
		}
		response.Body = &deploymentPageBody{ReadCloser: response.Body, remaining: deploymentPageBytes}
	}
	return response, nil
}

type deploymentPageBody struct {
	io.ReadCloser
	remaining int64
}

func (b *deploymentPageBody) Read(buffer []byte) (int, error) {
	if b.remaining < 0 {
		return 0, scanFailure("limit_exceeded")
	}
	if int64(len(buffer)) > b.remaining+1 {
		buffer = buffer[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(buffer)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		return n, scanFailure("limit_exceeded")
	}
	return n, err
}
