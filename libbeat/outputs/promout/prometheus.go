package promout

import (
	"net/http"
	"strconv"

	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/common/op"
	"github.com/elastic/beats/libbeat/outputs"
	"github.com/elastic/beats/libbeat/outputs/mode"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/vjeantet/grok"
)

var (
	agentInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "beat_info",
			Help: "filebeat info . ",
		},
		[]string{"instance", "version", "agentId"},
	)

	// 延迟
	httpLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_mseconds",
			Help:    "HTTP request latency distributions.",
			Buckets: []float64{50, 100, 250, 500, 750, 1000, 1500, 2000, 5000, 10000, 30000},
		},
		[]string{"method", "instance", "agentId"},
	)

	// 请求数统计 （含方法和状态）
	httpCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_request_total",
			Help: "Http request total count .",
		},
		[]string{"method", "code", "instance", "agentId"},
	)

	// 响应数
	httpResponse = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_response_status_total",
			Help: "Http response status total .",
		},
		[]string{"status", "instance", "agentId"},
	)

	// 吞吐量
	httpThroughput = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_body_bytes_sent_total",
			Help: "Http body bytes sent total .",
		},
		[]string{"method", "instance", "agentId"},
	)

	g *grok.Grok
)

type promOutput struct {
	mode mode.ConnectionMode
	addr *string
}

// 初始化
func init() {
	outputs.RegisterOutputPlugin("prometheus", New)
	// todo

	prometheus.MustRegister(httpLatency)
	prometheus.MustRegister(agentInfo)
	prometheus.MustRegister(httpCount)
	prometheus.MustRegister(httpResponse)
	prometheus.MustRegister(httpThroughput)

	http.Handle("/metrics", promhttp.Handler())

	g, _ = grok.New()
}

func New(beatName string, cfg *common.Config, _ int) (outputs.Outputer, error) {
	config := defaultConfig
	if err := cfg.Unpack(&config); err != nil {
		return nil, err
	}

	if err := cfg.Unpack(&config); err != nil {
		return nil, err
	}
	NewPrometheusServer(config.Addr)
	output := &promOutput{
		addr: &config.Addr,
	}
	return output, nil
}

// 启动prometheus server监听addr
func NewPrometheusServer(addr string) {
	go func() {
		log.Fatal(http.ListenAndServe(addr, nil))
	}()
}

// impl outputer
func (out *promOutput) Close() error {
	return nil
}

//消费Event
func (out *promOutput) PublishEvent(
	trans op.Signaler,
	opts outputs.Options,
	data outputs.Data,
) error {

	var (
		event      = data.Event
		version    string
		hostname   string
		agentId    string // nessa
		methodName string
		respStatus = event["status"].(string)
	)
	// 获取beat信息
	switch fields := event["beat"].(type) {
	case interface{}:
		var info = fields.(common.MapStr)
		version = info["version"].(string)
		hostname = info["hostname"].(string)
	}

	// check agent
	if _, ok := event["agentId"]; !ok {
		return errors.New("agentId in log is required .")
	} else {
		agentId = event["agentId"].(string)
	}

	// check method
	if _, ok := event["request_method"]; !ok {
		return errors.New("request_method in log is required .")
	} else {
		methodName = event["request_method"].(string)
	}

	// check method
	if _, ok := event["status"]; !ok {
		return errors.New("response status in log is required .")
	} else {
		httpResponse.WithLabelValues(respStatus, hostname, agentId).Inc()
		httpCount.WithLabelValues(methodName, respStatus, hostname, agentId).Inc()
	}

	// check duration
	if _, ok := event["request_time_seconds"]; ok {
		val, convErr := strconv.ParseFloat(event["request_time_seconds"].(string), 64)
		if convErr != nil {
			return convErr
		}
		httpLatency.WithLabelValues(methodName, hostname, agentId).Observe(val * 1000)
	} else if _, ok := event["request_time_microseconds"]; ok {
		val, convErr := strconv.ParseFloat(event["request_time_microseconds"].(string), 64)
		if convErr != nil {
			return convErr
		}
		httpLatency.WithLabelValues(methodName, hostname, agentId).Observe(val) // convert to micro seconds
	} else {
		return errors.New("request_duration in log is required .")
	}

	// 捕获到流量标签的数据
	if _, ok := event["body_bytes_sent"]; ok {
		val, convErr := strconv.ParseFloat(event["body_bytes_sent"].(string), 64)
		if convErr != nil {
			return convErr
		}
		httpThroughput.WithLabelValues(methodName, hostname, agentId).Add(val)
	}

	// 日志插件信息
	agentInfo.WithLabelValues(hostname, version, agentId).Set(1)

	// 提交消费状态
	op.SigCompleted(trans)
	return nil
}
