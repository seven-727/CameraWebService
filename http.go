package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/deepch/vdk/codec/h264parser"
	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"
)

func serveHTTP() {
	router := gin.Default()
	router.LoadHTMLGlob("web/templates/*")
	router.GET("/", func(c *gin.Context) {
		first, all := config.list()
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"port":     config.Server.HTTPPort,
			"suuid":    first,
			"suuidMap": all,
			"version":  time.Now().String(),
		})
	})
	router.GET("/player/:suuid", func(c *gin.Context) {
		_, all := config.list()
		sort.Strings(all)
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"port":     config.Server.HTTPPort,
			"suuid":    c.Param("suuid"),
			"suuidMap": all,
			"version":  time.Now().String(),
		})
	})
	router.POST("/v1/camera/webRTC", webRTC)
	router.GET("/v1/camera/url", func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		ip := c.PostForm("ip")
		port := c.DefaultPostForm("port", "554")
		user := c.PostForm("user")
		password := c.PostForm("password")
		camera := c.PostForm("camera")
		ok, url := config.formURL(camera, ip, port, user, password)
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
		url := c.PostForm("url")
		if !streams.ext(url) {
			streams.addURL(url)
		}
		t := time.Now()
		for {
			if streams.extCodecs(url) {
				break
			}
			t2 := time.Now().Sub(t)
			if t2 > 10*1000 {
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
				c.String(http.StatusInternalServerError, "摄像机编码错误")
				return
			}
		}
	})
	router.StaticFS("/static", http.Dir("web/static"))
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
	if streams.extCodecs(url) {
		/*

			Get Codecs INFO

		*/
		codecs := streams.getCodecs(url)
		if codecs == nil {
			log.Println("Codec error")
			return
		}
		sps := codecs[0].(h264parser.CodecData).SPS()
		pps := codecs[0].(h264parser.CodecData).PPS()
		/*

			Recive Remote SDP as Base64

		*/
		sd, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			log.Println("DecodeString error", err)
			return
		}
		/*

			Create Media MediaEngine

		*/

		mediaEngine := webrtc.MediaEngine{}
		offer := webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  string(sd),
		}
		err = mediaEngine.PopulateFromSDP(offer)
		if err != nil {
			log.Println("PopulateFromSDP error", err)
			return
		}

		var payloadType uint8
		for _, videoCodec := range mediaEngine.GetCodecsByKind(webrtc.RTPCodecTypeVideo) {
			if videoCodec.Name == "H264" && strings.Contains(videoCodec.SDPFmtpLine, "packetization-mode=1") {
				payloadType = videoCodec.PayloadType
				break
			}
		}
		if payloadType == 0 {
			log.Println("Remote peer does not support H264")
			return
		}
		if payloadType != 126 {
			log.Println("Video might not work with codec", payloadType)
		}
		log.Println("Work payloadType", payloadType)
		api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

		peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
			ICEServers: []webrtc.ICEServer{
				{
					URLs: []string{"stun:stun.l.google.com:19302"},
				},
			},
		})
		if err != nil {
			log.Println("NewPeerConnection error", err)
			return
		}
		/*

			ADD KeepAlive Timer

		*/
		timer1 := time.NewTimer(time.Second * 5)
		peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
			// Register text message handling
			d.OnMessage(func(msg webrtc.DataChannelMessage) {
				//fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
				timer1.Reset(5 * time.Second)
			})
		})
		/*

			ADD Video Track

		*/
		videoTrack, err := peerConnection.NewTrack(payloadType, rand.Uint32(), "video", url+"_pion")
		if err != nil {
			log.Fatalln("NewTrack", err)
		}
		_, err = peerConnection.AddTransceiverFromTrack(videoTrack,
			webrtc.RtpTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionSendonly,
			},
		)
		if err != nil {
			log.Println("AddTransceiverFromTrack error", err)
			return
		}
		_, err = peerConnection.AddTrack(videoTrack)
		if err != nil {
			log.Println("AddTrack error", err)
			return
		}
		/*

			ADD Audio Track

		*/
		// var audioTrack *webrtc.Track
		// if len(codecs) > 1 && (codecs[1].Type() == av.PCM_ALAW || codecs[1].Type() == av.PCM_MULAW) {
		// 	switch codecs[1].Type() {
		// 	case av.PCM_ALAW:
		// 		audioTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypePCMA, rand.Uint32(), "audio", suuid+"audio")
		// 	case av.PCM_MULAW:
		// 		audioTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypePCMU, rand.Uint32(), "audio", suuid+"audio")
		// 	}
		// 	if err != nil {
		// 		log.Println(err)
		// 		return
		// 	}
		// 	_, err = peerConnection.AddTransceiverFromTrack(audioTrack,
		// 		webrtc.RtpTransceiverInit{
		// 			Direction: webrtc.RTPTransceiverDirectionSendonly,
		// 		},
		// 	)
		// 	if err != nil {
		// 		log.Println("AddTransceiverFromTrack error", err)
		// 		return
		// 	}
		// 	_, err = peerConnection.AddTrack(audioTrack)
		// 	if err != nil {
		// 		log.Println(err)
		// 		return
		// 	}
		// }
		if err := peerConnection.SetRemoteDescription(offer); err != nil {
			log.Println("SetRemoteDescription error", err, offer.SDP)
			return
		}
		answer, err := peerConnection.CreateAnswer(nil)
		if err != nil {
			log.Println("CreateAnswer error", err)
			return
		}

		if err = peerConnection.SetLocalDescription(answer); err != nil {
			log.Println("SetLocalDescription error", err)
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
		return
	}
}
