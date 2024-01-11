package venstar

import (
	"bytes"
	"errors"
	"log"
	"net"
	//"net/http"
	//"net/url"
	"os"
	"time"
)

func Discover(timeout time.Duration) (chan *Device, error) {
	raddr := &net.UDPAddr{
		IP: net.ParseIP("239.255.255.250"),
		Port: 1900,
	}
	reqBuf := bytes.NewBuffer(nil)
	reqBuf.Write([]byte("M-SEARCH * HTTP/1.1\r\n"))
	reqBuf.Write([]byte("HOST: "+raddr.String()+"\r\n"))
	reqBuf.Write([]byte("MAN: \"ssdp:discover\"\r\n"))
	reqBuf.Write([]byte("ST: ssdp:all\r\n"))
	reqBuf.Write([]byte("MX: 10\r\n"))
	reqBuf.Write([]byte("\n"))
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	ch := make(chan *Device, 10)
	go func() {
		defer conn.Close()
		defer close(ch)
		buf := make([]byte, 1500)
		conn.SetDeadline(time.Now().Add(timeout))
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				if !errors.Is(err, os.ErrDeadlineExceeded) {
					log.Println("listener read error:", err)
				}
				return
			}
			info := make([]byte, n)
			copy(info, buf[:n])
			dev, err := NewDevice(info)
			if err != nil {
				log.Println("error parsing device:", err)
			} else if dev != nil {
				ch <- dev
			}
		}
	}()
	_, err = conn.WriteTo(reqBuf.Bytes(), raddr)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ch, nil
}
