package adapter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/netguard"
)

// externalStockBodyLimit 是外部库存响应允许读取的最大字节数，超出后视为失败并不保存正文。
const externalStockBodyLimit = 1 << 20

// ExternalStockFetcher 用出站防护客户端读取外部可用库存。
type ExternalStockFetcher struct{}

// NewExternalStockFetcher 创建外部库存读取器。
func NewExternalStockFetcher() *ExternalStockFetcher {
	return &ExternalStockFetcher{}
}

// ReadAvailable 发起一次 GET。错误只包含状态码，不包含响应正文或请求头。
func (fetcher *ExternalStockFetcher) ReadAvailable(ctx context.Context, request itemapp.ExternalStockRead) (int, error) {
	if fetcher == nil {
		return 0, fmt.Errorf("%w: 读取器未初始化", itemapp.ErrExternalStockFetch)
	}
	// timeout 是本次请求超时，空值使用默认 20 秒。
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = itemapp.ExternalStockDefaultTimeout
	}
	// client、err 是按地址校验后的出站客户端。
	client, err := netguard.ConfiguredEndpointHTTPClient(request.URL, timeout)
	if err != nil {
		return 0, fmt.Errorf("%w: 地址无效", itemapp.ErrExternalStockFetch)
	}
	// httpRequest 是不携带正文的 GET。
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: 无法创建请求", itemapp.ErrExternalStockFetch)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", itemapp.NormalizeExternalStockUserAgent(request.UserAgent))
	// name、value 是一条自定义请求头。User-Agent 以独立字段为准，避免被空值覆盖。
	for name, value := range request.Headers {
		if strings.EqualFold(name, "User-Agent") || strings.TrimSpace(name) == "" {
			continue
		}
		httpRequest.Header.Set(name, value)
	}
	// response、doErr 是外部接口响应和传输错误。
	response, doErr := client.Do(httpRequest)
	if doErr != nil {
		return 0, fmt.Errorf("%w: 网络请求失败", itemapp.ErrExternalStockFetch)
	}
	defer response.Body.Close()
	// body、readErr 是截断后的响应正文。超限或读取失败都不把正文写入错误。
	body, readErr := io.ReadAll(io.LimitReader(response.Body, externalStockBodyLimit+1))
	if readErr != nil {
		return 0, fmt.Errorf("%w: 读取响应失败", itemapp.ErrExternalStockFetch)
	}
	if len(body) > externalStockBodyLimit {
		return 0, fmt.Errorf("%w: 响应过大", itemapp.ErrExternalStockFetch)
	}
	if response.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(string(body)), "1010") {
		return 0, fmt.Errorf("%w: HTTP 403，Cloudflare 1010，请使用桌面浏览器 User-Agent", itemapp.ErrExternalStockFetch)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("%w: HTTP %d", itemapp.ErrExternalStockFetch, response.StatusCode)
	}
	return itemapp.ParseAvailableCount(body, request.JSONPath)
}

// 确保读取器实现应用端口。
var _ itemapp.ExternalStockReader = (*ExternalStockFetcher)(nil)
