package transport

import "github.com/prometheus/client_golang/prometheus"

type TransportMetrics struct {
	transportType string
	outBytes      *prometheus.CounterVec
	inBytes       *prometheus.CounterVec
}

// TODO: Think about a smart way to handle those references.
var incomingCounterVec = GetIncomingCounterVec()
var outgoingCounterVec = GetOutgoingCounterVec()

func GetIncomingCounterVec() *prometheus.CounterVec {

	inBytesCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nexus_transport_bytes_incoming",
			Help: "Total incoming bytes",
		},
		[]string{"transport_type"},
	)

	prometheus.MustRegister(inBytesCounter)
	return inBytesCounter
}

func GetOutgoingCounterVec() *prometheus.CounterVec {

	outgoingBytesCounterVec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nexus_transport_bytes_outgoing",
			Help: "Total outgoing bytes",
		},
		[]string{"transport_type"},
	)

	prometheus.MustRegister(outgoingBytesCounterVec)
	return outgoingBytesCounterVec
}

func NewTransportMetrics(transportType string) *TransportMetrics {

	transportMetrics := TransportMetrics{
		transportType: transportType,
		inBytes:       incomingCounterVec,
		outBytes:      outgoingCounterVec,
	}

	return &transportMetrics
}

func (t TransportMetrics) CountIncoming(bytesNum int) {
	t.inBytes.WithLabelValues(t.transportType).Add(float64(bytesNum))
}

func (t TransportMetrics) CountOutgoing(bytesNum int) {
	t.outBytes.WithLabelValues(t.transportType).Add(float64(bytesNum))
}
