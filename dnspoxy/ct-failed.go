package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// what we dump if --json
type Record struct {
	Proto    string `json:"proto"`
	SrcIP    string `json:"src_ip"`
	SrcPort  uint16 `json:"src_port"`
	DstIP    string `json:"dst_ip"`
	DstPort  uint16 `json:"dst_port"`
	Attempts uint64 `json:"attempts"`
	Timeout  uint32 `json:"timeout_sec"`
}

func ctFailed() {
	jsonOut := flag.Bool("json", false, "emit ND-json (one object per line)")
	family := flag.Int("family", unix.AF_INET, "address family (default IPv4)")
	flag.Parse()

	flows, err := netlink.ConntrackTableList(
		netlink.ConntrackTable,      // whole table
		netlink.InetFamily(*family), // AF_INET/INET6
	) // :contentReference[oaicite:0]{index=0}
	if err != nil {
		log.Fatalf("conntrack dump failed: %v", err)
	}

	for _, f := range flows {
		// we treat “no packets back” as “connection never got a reply”
		if f.Reverse.Packets != 0 {
			continue
		}

		if *jsonOut {
			rec := Record{
				Proto:    protoName(f.Forward.Protocol),
				SrcIP:    f.Forward.SrcIP.String(),
				SrcPort:  f.Forward.SrcPort,
				DstIP:    f.Forward.DstIP.String(),
				DstPort:  f.Forward.DstPort,
				Attempts: f.Forward.Packets,
				Timeout:  f.TimeOut,
			}
			_ = json.NewEncoder(os.Stdout).Encode(rec)
			continue
		}

		fmt.Printf(
			"%-3s %s:%d -> %s:%d  attempts=%d  timeout=%ds\n",
			protoName(f.Forward.Protocol),
			f.Forward.SrcIP, f.Forward.SrcPort,
			f.Forward.DstIP, f.Forward.DstPort,
			f.Forward.Packets,
			f.TimeOut,
		)
	}
}

func protoName(p uint8) string {
	switch p {
	case unix.IPPROTO_TCP:
		return "tcp"
	case unix.IPPROTO_UDP:
		return "udp"
	default:
		return fmt.Sprintf("%d", p)
	}
}
