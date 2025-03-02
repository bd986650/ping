package main

import (
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var (
	count, szpacket int
	tos, ttl        int
	srcAddr         string
)

func init() {
	flag.IntVar(&count, "c", math.MaxInt, "number of requests to send")
	flag.IntVar(&szpacket, "s", 56, "packet size")
	flag.IntVar(&tos, "Q", 0, "Quality of Service")
	flag.IntVar(&ttl, "t", 64, "IP Time to Live")
	flag.StringVar(&srcAddr, "I", "", "interface address or name")
	flag.Parse()
}

func main() {
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: go run main.go <hostname>")
		os.Exit(1)
	}

	time.AfterFunc(10*time.Second, func() {
		fmt.Println("Время выполнения пинга истекло!")
		os.Exit(0)
	})

	var zone string
	hostname := flag.Arg(0)

	if idx := strings.Index(hostname, "%"); idx != -1 {
		s := strings.Split(hostname, "%")
		if len(s) != 2 {
			fmt.Fprintln(os.Stderr, "Error: invalid hostname")
			os.Exit(2)
		}
		hostname = s[0]
		zone = s[1]
	}

	var tgtAddr net.IP
	if tgtAddr = net.ParseIP(hostname); tgtAddr == nil {
		addrs, err := net.LookupHost(hostname)
		if err != nil || len(addrs) == 0 {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(2)
		}
		tgtAddr = net.ParseIP(addrs[0])
	}

	fmt.Println("Начинаем пинг для", tgtAddr)

	var err error
	if tgtAddr.To4() != nil {
		err = ping4(tgtAddr, srcAddr, count, szpacket, tos, ttl)
	} else {
		err = ping6(tgtAddr, zone, srcAddr, count, szpacket, tos, ttl)
	}
	if err != nil {
		fmt.Println("Ping error:", err)
	}
}

// === IPv6 PING ===
func ping6(target net.IP, zone, source string, nmpackets, szpacket, tos, ttl int) error {
	sourceAddr := "::"
	if len(source) > 0 {
		sourceAddr = source
	}
	if len(zone) > 0 {
		sourceAddr += "%" + zone
	}

	netconn, err := net.ListenPacket("ip6:ipv6-icmp", sourceAddr)
	if err != nil {
		return err
	}
	defer netconn.Close()

	conn := ipv6.NewPacketConn(netconn)
	defer conn.Close()
	conn.SetHopLimit(ttl)
	conn.SetTrafficClass(tos)
	conn.SetControlMessage(ipv6.FlagHopLimit, true)

	// Создаем канал для синхронизации завершения горутин
	done := make(chan bool, 2)

	// Запускаем горутины
	go icmpReceiver6(conn, target, nmpackets, done)
	go icmpSender6(conn, target, nmpackets, szpacket, done)

	// Ждем, пока все горутины завершатся
	<-done
	<-done

	return nil
}

func icmpSender6(c *ipv6.PacketConn, target net.IP, nmpackets, _ int, done chan bool) {
	id := os.Getpid() & 0xffff
	seqnumber := 1

	for i := 0; i < nmpackets; i++ {
		msg := &icmp.Message{
			Type: ipv6.ICMPTypeEchoRequest,
			Code: 0,
			Body: &icmp.Echo{
				ID:   id,
				Seq:  seqnumber,
				Data: []byte("ping"),
			},
		}
		wb, _ := msg.Marshal(nil)
		addr := &net.UDPAddr{IP: target}

		c.WriteTo(wb, nil, addr)
		seqnumber++
		fmt.Println("Отправлен ICMP-запрос для IPv6 на", target)
		time.Sleep(1 * time.Second)
	}

	// Сообщаем, что горутина завершена
	done <- true
}

func icmpReceiver6(c *ipv6.PacketConn, target net.IP, nmpackets int, done chan bool) {
	buffer := make([]byte, 1500)
	id := os.Getpid() & 0xffff

	for i := 0; i < nmpackets; i++ {
		n, _, _, err := c.ReadFrom(buffer)
		if err != nil {
			continue
		}

		msg, _ := icmp.ParseMessage(58, buffer[:n])
		if echoReply, ok := msg.Body.(*icmp.Echo); ok && echoReply.ID == id {
			fmt.Printf("Ответ от %s: seq=%d\n", target, echoReply.Seq)
		}
	}

	// Сообщаем, что горутина завершена
	done <- true
}

// === IPv4 PING ===
func ping4(target net.IP, source string, nmpackets, szpacket, tos, ttl int) error {
	netconn, err := net.ListenPacket("ip4:icmp", source)
	if err != nil {
		return err
	}
	defer netconn.Close()

	conn := ipv4.NewPacketConn(netconn)
	defer conn.Close()
	conn.SetTOS(tos)
	conn.SetTTL(ttl)

	// Создаем канал для синхронизации завершения горутин
	done := make(chan bool, 2)

	// Запускаем горутины
	go icmpReceiver4(conn, target, nmpackets, done)
	go icmpSender4(conn, target, nmpackets, szpacket, done)

	// Ждем, пока все горутины завершатся
	<-done
	<-done

	return nil
}

func icmpSender4(c *ipv4.PacketConn, target net.IP, nmpackets, _ int, done chan bool) {
	id := os.Getpid() & 0xffff
	seqnumber := 1

	for i := 0; i < nmpackets; i++ {
		msg := &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   id,
				Seq:  seqnumber,
				Data: []byte("ping"),
			},
		}
		wb, _ := msg.Marshal(nil)
		addr := &net.IPAddr{IP: target}

		c.WriteTo(wb, nil, addr)
		seqnumber++
		fmt.Println("Отправлен ICMP-запрос для IPv4 на", target)
		time.Sleep(1 * time.Second)
	}

	// Сообщаем, что горутина завершена
	done <- true
}

func icmpReceiver4(c *ipv4.PacketConn, target net.IP, nmpackets int, done chan bool) {
	buffer := make([]byte, 1500)
	id := os.Getpid() & 0xffff

	for i := 0; i < nmpackets; i++ {
		n, _, _, err := c.ReadFrom(buffer)
		if err != nil {
			continue
		}

		msg, _ := icmp.ParseMessage(1, buffer[:n])
		if echoReply, ok := msg.Body.(*icmp.Echo); ok && echoReply.ID == id {
			fmt.Printf("Ответ от %s: seq=%d\n", target, echoReply.Seq)
		}
	}

	// Сообщаем, что горутина завершена
	done <- true
}
