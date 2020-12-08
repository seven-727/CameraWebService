package main

import (
	"log"
	"time"

	"github.com/deepch/vdk/format/rtsp"
)

// rtsp streams
func rtspStreams(url string, ch chan string) {
	for {
		if listenRTSPStreams(url, ch) {
			// 通道关闭，退出
			break
		}
		log.Println("connect", url)
		rtsp.DebugRtsp = true
		session, err := rtsp.Dial(url)
		if err != nil {
			log.Println(url, err)
			time.Sleep(3 * time.Second)
			continue
		}

		session.RtpKeepAliveTimeout = 10 * time.Second
		if err != nil {
			log.Println(url, err)
			time.Sleep(3 * time.Second)
			continue
		}
		codec, err := session.Streams()
		if err != nil {
			log.Println(url, err)
			time.Sleep(3 * time.Second)
			continue
		}
		streams.addCodecs(url, codec)
		for {
			if listenRTSPStreams(url, ch) {
				// 直接退出
				err = session.Close()
				return
			}
			pkt, err := session.ReadPacket()
			if err != nil {
				log.Println(url, err)
				break
			}
			streams.cast(url, pkt)
		}
		err = session.Close()
		if err != nil {
			log.Println("session Close error", err)
		}

		log.Println(url, "reconnect wait 3s")
		time.Sleep(3 * time.Second)
	}
}

// 监听通道
func listenRTSPStreams(url string, ch chan string) bool {
	if !streams.ext(url) {
		return true
	}
	select {
	case v, ok := <-ch:
		if !ok {
			// channel is closed
			return true
		}
		if v == "STOP" {
			// stop channel
			return true
		}
	}
	return false
}
