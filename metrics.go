package main

import (
	"expvar"
	"net/http"
)

var totalRequestCount expvarInt
var concurrentRequestCount expvarInt
var maxConcurrentRequestCount expvarMaxInt

func publishExpvarMetrics() {
	metrics := expvar.Map{}
	metrics.Set("totalRequestCount", &totalRequestCount)
	metrics.Set("concurrentRequestCount", &concurrentRequestCount)
	metrics.Set("maxConcurrentRequestCount", &maxConcurrentRequestCount)
	expvar.Publish("metrics", &metrics)
}

type metricsMiddleware struct {
	nextHandler http.HandlerFunc
}

func (m metricsMiddleware) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	m.onHandleRequestStart()
	defer m.onHandleRequestFinish()
	m.nextHandler(writer, request)
}

func (m metricsMiddleware) onHandleRequestStart() {
	count := concurrentRequestCount.Add(1)
	totalRequestCount.Add(1)
	maxConcurrentRequestCount.Update(count)
}

func (m metricsMiddleware) onHandleRequestFinish() {
	concurrentRequestCount.Add(-1)
}
