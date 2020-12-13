package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/deepch/vdk/codec/h264parser"
	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"
)

func serverHTTP() {
	router := gin.Default()
	router.POST("/v1/camera/webRTC", webRTC)
	router.GET("/v1/camera/rtsp", func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		ip := c.Query("ip")
		port := c.DefaultQuery("port", "554")
		user := c.Query("user")
		password := c.Query("pwd")
		chn := c.DefaultQuery("chn", "1")
		codec := c.DefaultQuery("codec", "h264")
		camera := c.DefaultQuery("camera", "hikvision-old")
		ok, url := config.formURL(camera, ip, port, user, password, chn, codec)
		if !ok {
			c.String(http.StatusInternalServerError, "没有配置摄像机类型")
		}
		m := map[string]string{"url": url}
		b, err := json.Marshal(m)
		if err == nil {
			_, err = c.Writer.Write(b)
			if err == nil {
				log.Println("Write Url Info error", err)
				return
			}
		}
		c.String(http.StatusOK, url)
	})
	router.GET("/v1/camera/codec", func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		url := c.Query("url")
		if !streams.ext(url) {
			streams.addURL(url)
		}
		t := time.Now()
		for {
			if streams.extCodecs(url) {
				break
			}
			t2 := time.Now().Sub(t)
			if t2.Seconds() > 10 {
				c.String(http.StatusInternalServerError, "摄像机连接失败")
				streams.delURL(url)
				return
			}
		}

		codecs := streams.getCodecs(url)
		if codecs == nil {
			return
		}
		b, err := json.Marshal(codecs)
		if err == nil {
			_, err = c.Writer.Write(b)
			if err == nil {
				log.Println("Write Codec Info error", err)
				return
			}
		}
	})
	err := router.Run(config.Server.HTTPPort)
	if err != nil {
		log.Fatalln("Start HTTP Server error", err)
	}
}

func webRTC(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	data := c.PostForm("data")
	url := c.PostForm("url")
	log.Println("Request", url)
	if !streams.extCodecs(url) {
		c.String(http.StatusInternalServerError, "Codec error")
		return
	}
	//	Get Codecs INFO
	codecs := streams.getCodecs(url)
	sps := codecs[0].(h264parser.CodecData).SPS()
	pps := codecs[0].(h264parser.CodecData).PPS()
	// Receive Remote SDP as Base64
	sd, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		c.String(http.StatusInternalServerError, "DecodeString error", err)
		return
	}
	peerConnection, payloadType, err := newPeerConnection(sd, url)
	// ADD KeepAlive Timer
	timer1 := time.NewTimer(time.Second * 5)
	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		// Register text message handling
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			//fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
			timer1.Reset(5 * time.Second)
		})
	})
	// ADD Video Track
	videoTrack, err := newVideoTrack(peerConnection, payloadType)
	if err != nil {
		c.String(http.StatusInternalServerError, "newVideoTrack error", err)
		return
	}
	// ADD Audio Track
	// audioTrack, err := newAudioTrack(peerConnection, codecs)
	// if err != nil {
	// 	c.String(http.StatusInternalServerError, "newAudioTrack error", err)
	// 	return
	// }

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		c.String(http.StatusInternalServerError, "CreateAnswer error", err)
		return
	}
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		c.String(http.StatusInternalServerError, "SetLocalDescription error", err)
		return
	}
	_, err = c.Writer.Write([]byte(base64.StdEncoding.EncodeToString([]byte(answer.SDP))))
	if err != nil {
		log.Println("Writer SDP error", err)
		return
	}
	control := make(chan bool, 10)
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Printf("Connection State has changed %s \n", connectionState.String())
		if connectionState != webrtc.ICEConnectionStateConnected {
			log.Println("Client Close Exit")
			err := peerConnection.Close()
			if err != nil {
				log.Println("peerConnection Close error", err)
			}
			control <- true
			return
		}
		if connectionState == webrtc.ICEConnectionStateConnected {
			go func() {
				uuid, ch := streams.addViewer(url)
				log.Println("start stream", url, "client", uuid)
				defer func() {
					log.Println("stop stream", url, "client", uuid)
					defer streams.delViewer(url, uuid)
				}()
				var Vpre time.Duration
				var start bool
				timer1.Reset(5 * time.Second)
				for {
					select {
					case <-timer1.C:
						log.Println("Client Close Keep-Alive Timer")
						peerConnection.Close()
					case <-control:
						return
					case pck := <-ch:
						//timer1.Reset(2 * time.Second)
						if pck.IsKeyFrame {
							start = true
						}
						if !start {
							continue
						}
						if pck.IsKeyFrame {
							pck.Data = append([]byte("\000\000\001"+string(sps)+"\000\000\001"+string(pps)+"\000\000\001"), pck.Data[4:]...)

						} else {
							pck.Data = pck.Data[4:]
						}
						var Vts time.Duration
						if pck.Idx == 0 && videoTrack != nil {
							if Vpre != 0 {
								Vts = pck.Time - Vpre
							}
							samples := uint32(90000 / 1000 * Vts.Milliseconds())
							err := videoTrack.WriteSample(media.Sample{Data: pck.Data, Samples: samples})
							if err != nil {
								return
							}
							Vpre = pck.Time
							// } else if pck.Idx == 1 && audioTrack != nil {
							// 	err := audioTrack.WriteSample(media.Sample{Data: pck.Data, Samples: uint32(len(pck.Data))})
							// 	if err != nil {
							// 		return
							// 	}
						}
					}
				}

			}()
		}
	})
}
