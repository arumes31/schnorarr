package sync

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"time"

	"schnorarr/internal/internalapi"
)

func receiverAPIRequest(
	ctx context.Context,
	method string,
	endpoint string,
	query url.Values,
	timeout time.Duration,
) (*http.Response, error) {
	request, err := internalapi.NewRequest(ctx, os.Getenv, method, endpoint, query)
	if err != nil {
		return nil, err
	}
	client, err := internalapi.NewClient(os.Getenv, timeout)
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}
