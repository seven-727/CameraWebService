package main

import (
	"errors"
	"log"
	"math/rand"
	"strings"

	"github.com/deepch/vdk/av"
	"github.com/pion/webrtc/v2"
)

func newPeerConnection(sd []byte, url string) (*webrtc.PeerConnection, uint8, error) {
	// Create Media MediaEngine
	mediaEngine := webrtc.MediaEngine{}
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(sd),
	}
	err := mediaEngine.PopulateFromSDP(offer)
	if err != nil {
		log.Println("PopulateFromSDP error", err)
		return nil, 0, err
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
		errx := errors.New("payloadType is 0")
		return nil, 0, errx
	}
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
		return nil, 0, err
	}

	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		log.Println("SetRemoteDescription error", err, offer.SDP)
		return nil, 0, err
	}
	return peerConnection, payloadType, err
}

func newVideoTrack(peerConnection *webrtc.PeerConnection, payloadType uint8) (*webrtc.Track, error) {
	videoTrack, err := peerConnection.NewTrack(payloadType, rand.Uint32(), "video", "")
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
		return nil, err
	}
	_, err = peerConnection.AddTrack(videoTrack)
	if err != nil {
		log.Println("AddTrack error", err)
		return nil, err
	}
	return videoTrack, err
}

func newAudioTrack(peerConnection *webrtc.PeerConnection, codecs []av.CodecData) (*webrtc.Track, error) {
	var audioTrack *webrtc.Track
	var err error
	if len(codecs) > 1 && (codecs[1].Type() == av.PCM_ALAW || codecs[1].Type() == av.PCM_MULAW) {
		switch codecs[1].Type() {
		case av.PCM_ALAW:
			audioTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypePCMA, rand.Uint32(), "audio", "")
		case av.PCM_MULAW:
			audioTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypePCMU, rand.Uint32(), "audio", "")
		}
		if err != nil {
			log.Println(err)
			return nil, err
		}
		_, err = peerConnection.AddTransceiverFromTrack(audioTrack,
			webrtc.RtpTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionSendonly,
			},
		)
		if err != nil {
			log.Println("AddTransceiverFromTrack error", err)
			return nil, err
		}
		_, err = peerConnection.AddTrack(audioTrack)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		return audioTrack, err
	}
	err = errors.New("codecs error")
	return nil, err
}
