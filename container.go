package main

/*
	container 数据容器，服务中所有数据结构及接口都在数据容器中获取
*/
import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"sync"

	"github.com/deepch/vdk/av"
)

// 初始化配置文件
var path = "../conf/conf.json"
var config = loadJSON(path)
var streams StreamsSt

// 读取json文件
func loadJSON(path string) *ConfigST {
	var tmp ConfigST
	buf, err := ioutil.ReadFile(path)
	if err != nil {
		log.Panicln("load config conf failed: ", err)
	}
	err = json.Unmarshal(buf, &tmp)
	if err != nil {
		log.Panicln("decode config file failed:", string(buf), err)
	}
	log.Println("http port", tmp.Server.HTTPPort)
	for i, v := range tmp.URL {
		log.Println(i, v)
	}

	return &tmp
}

//ConfigST struct
type ConfigST struct {
	Server ServerST `json:"server"`
	/*
		配置的url模板，ip、端口、用户、密码、编码格式、通道
		用IP/PORT/USER/PASSWORD/CODEC/CHANNEL代替，根据
		传入参数替换
	*/
	URL map[string]string
}

//ServerST struct
type ServerST struct {
	HTTPPort string `json:"http_port"`
}

// list all type(rtsp url)
func (element *ConfigST) list() (string, []string) {
	var res []string
	var first string
	for k := range element.URL {
		if first == "" {
			first = k
		}
		res = append(res, k)
	}
	return first, res
}

//camera url is exist?
func (element *ConfigST) ext(url string) bool {
	_, ok := element.URL[url]
	return ok
}

//replace ip,port,user,password
func (element *ConfigST) formURL(urlType, ip, port, user, password string) (bool, string) {
	if !element.ext(urlType) {
		return false, ""
	}
	url := element.URL[urlType]
	strings.Replace(url, "ip", ip, 1)
	strings.Replace(url, "port", port, 1)
	strings.Replace(url, "user", user, 1)
	strings.Replace(url, "password", password, 1)
	return true, url
}

//StreamsSt rtsp streams
type StreamsSt struct {
	sync.RWMutex
	Streams map[string]StreamST // k:rtsp url, v:stream
}

//StreamST struct
type StreamST struct {
	Status bool             // rtsp connect status
	Codecs []av.CodecData   // av codecs
	Client map[string]viwer // webrtc connected viewer
	ch     chan string      // stream chan, func rtspStreams中接收信息，用于退出goroutine
}

type viwer struct {
	ch chan av.Packet // chan rstp -> webrtc
}

// 将rstp流发送到各个已连接的webrtc viewer通道
func (element *StreamsSt) cast(uuid string, pck av.Packet) {
	for _, viewer := range element.Streams[uuid].Client {
		if len(viewer.ch) < cap(viewer.ch) {
			viewer.ch <- pck
		}
	}
}

// connect url is exist?
func (element *StreamsSt) ext(url string) bool {
	_, ok := element.Streams[url]
	return ok
}

// get rtsp url
func (element *StreamsSt) getURL(url string) *StreamST {
	element.RLock()
	defer element.RUnlock()
	if !element.ext(url) {
		return nil
	}
	tmp := element.Streams[url]
	return &tmp
}

// add rtsp url
func (element *StreamsSt) addURL(url string) {
	element.Lock()
	defer element.Unlock()
	if element.ext(url) {
		return
	}
	tmp := StreamST{Client: make(map[string]viwer), ch: make(chan string)}
	element.Streams[url] = tmp
	go rtspStreams(url, element.Streams[url].ch)
}

// delete rtsp url
func (element *StreamsSt) delURL(url string) {
	element.Lock()
	defer element.Unlock()
	// 通知rtspStreams goroutine 关闭
	element.Streams[url].ch <- "STOP"
	delete(element.Streams, url)
}

// add connected rtsp codecs
func (element *StreamsSt) addCodecs(url string, codecs []av.CodecData) {
	element.Lock()
	defer element.Unlock()
	t := element.Streams[url]
	t.Status = true
	t.Codecs = codecs
	element.Streams[url] = t
}

// get connected rtsp codecs
func (element *StreamsSt) getCodecs(url string) []av.CodecData {
	element.RLock()
	defer element.RUnlock()
	return element.Streams[url].Codecs
}

// is codecs exist
func (element *StreamsSt) extCodecs(url string) bool {
	element.RLock()
	defer element.RUnlock()
	return element.Streams[url].Status
}

// add webrtc viewer
func (element *StreamsSt) addViewer(url string) (string, chan av.Packet) {
	element.Lock()
	defer element.Unlock()
	uuid := generateUUID()
	ch := make(chan av.Packet, 100)
	element.Streams[url].Client[uuid] = viwer{ch: ch}
	return uuid, ch
}

// delete webrtc viewer
func (element *StreamsSt) delViewer(url, suuid string) {
	element.Lock()
	defer element.Unlock()
	delete(element.Streams[url].Client, suuid)
	// no viewer close streams
	if len(element.Streams[url].Client) <= 0 {
		element.delURL(url)
	}
}

// list all connected url
func (element *StreamsSt) listURL() (string, []string) {
	element.RLock()
	defer element.RUnlock()
	var res []string
	var first string
	for k := range element.Streams {
		if first == "" {
			first = k
		}
		res = append(res, k)
	}
	return first, res
}

// generate uuid
func generateUUID() (uuid string) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}
	uuid = fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return uuid
}
