package httpclient

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientImpl_DoRetryWorks(t *testing.T) {
	count := 0
	requestBytes := []byte{12, 13}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		gotReqBytes, err := ioutil.ReadAll(req.Body)
		assert.NoError(t, err)
		assert.Equal(t, requestBytes, gotReqBytes)
		if count == 0 {
			rw.WriteHeader(http.StatusBadRequest)
		}
		// Otherwise 200 is returned
		count++
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURLs([]string{server.URL}))
	assert.NoError(t, err)

	_, err = client.Do(
		context.Background(),
		WithRawRequestBodyProvider(func() io.ReadCloser {
			return ioutil.NopCloser(bytes.NewReader(requestBytes))
		}),
		WithRequestMethod(http.MethodPost))
	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}
