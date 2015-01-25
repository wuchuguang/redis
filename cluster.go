package redis

import (
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"gopkg.in/bufio.v1"
)

const HashSlots = 16384

var (
	_ Cmder = (*ClusterSlotCmd)(nil)
)

//------------------------------------------------------------------------------

func (c *Client) ClusterSlots() *ClusterSlotCmd {
	req := NewClusterSlotCmd("CLUSTER", "slots")
	c.Process(req)
	return req
}

func (c *Client) ClusterNodes() *StringCmd {
	req := NewStringCmd("CLUSTER", "nodes")
	c.Process(req)
	return req
}

func (c *Client) ClusterMeet(host, port string) *StatusCmd {
	req := NewStatusCmd("CLUSTER", "meet", host, port)
	c.Process(req)
	return req
}

func (c *Client) ClusterReplicate(nodeID string) *StatusCmd {
	req := NewStatusCmd("CLUSTER", "replicate", nodeID)
	c.Process(req)
	return req
}

func (c *Client) ClusterAddSlots(slots ...int) *StatusCmd {
	args := make([]string, len(slots)+2)
	args[0] = "CLUSTER"
	args[1] = "addslots"
	for i, num := range slots {
		args[i+2] = strconv.Itoa(num)
	}
	req := NewStatusCmd(args...)
	c.Process(req)
	return req
}

func (c *Client) ClusterAddSlotsRange(min, max int) *StatusCmd {
	size := max - min + 1
	slots := make([]int, size)
	for i := 0; i < size; i++ {
		slots[i] = min + i
	}
	return c.ClusterAddSlots(slots...)
}

//------------------------------------------------------------------------------

type ClusterSlotCmd struct {
	*baseCmd

	val []ClusterSlotInfo
}

func NewClusterSlotCmd(args ...string) *ClusterSlotCmd {
	return &ClusterSlotCmd{
		baseCmd: newBaseCmd(args...),
	}
}

func (cmd *ClusterSlotCmd) Val() []ClusterSlotInfo {
	return cmd.val
}

func (cmd *ClusterSlotCmd) Result() ([]ClusterSlotInfo, error) {
	return cmd.Val(), cmd.Err()
}

func (cmd *ClusterSlotCmd) String() string {
	return cmdString(cmd, cmd.val)
}

func (cmd *ClusterSlotCmd) parseReply(rd *bufio.Reader) error {
	v, err := parseReply(rd, parseClusterSlotInfos)
	if err != nil {
		cmd.err = err
		return err
	}
	cmd.val = v.([]ClusterSlotInfo)
	return nil
}

//------------------------------------------------------------------------------

type ClusterOptions struct {
	// A seed-list of host:port addresses of known cluster nodes
	Addrs []string

	// An optional password
	Password string

	// The maximum number of total TCP connections to all
	// cluster nodes. Default: 60
	PoolSize int

	// Timeout settings
	DialTimeout, ReadTimeout, WriteTimeout, IdleTimeout time.Duration
}

func (opt *ClusterOptions) getPoolSize() int {
	if opt.PoolSize == 0 {
		return 60
	}
	return opt.PoolSize
}

func (opt *ClusterOptions) getDialTimeout() time.Duration {
	if opt.DialTimeout == 0 {
		return 5 * time.Second
	}
	return opt.DialTimeout
}

func (opt *ClusterOptions) options() *options {
	return &options{
		DB:       0,
		Password: opt.Password,

		DialTimeout:  opt.getDialTimeout(),
		ReadTimeout:  opt.ReadTimeout,
		WriteTimeout: opt.WriteTimeout,

		PoolSize:    opt.getPoolSize(),
		IdleTimeout: opt.IdleTimeout,
	}
}

//------------------------------------------------------------------------------

type ClusterSlotInfo struct {
	Min, Max int
	Addrs    []string
}

func parseClusterSlotInfos(rd *bufio.Reader, n int64) (interface{}, error) {
	infos := make([]ClusterSlotInfo, 0, n)
	for i := int64(0); i < n; i++ {
		viface, err := parseReply(rd, parseSlice)
		if err != nil {
			return nil, err
		}

		item, ok := viface.([]interface{})
		if !ok {
			return nil, fmt.Errorf("got %T, expected []interface{}", viface)
		} else if len(item) < 3 {
			return nil, fmt.Errorf("got %v, expected {int64, int64, string...}", item)
		}

		min, ok := item[0].(int64)
		if !ok || min < 0 || min > HashSlots {
			return nil, fmt.Errorf("got %v, expected {int64, int64, string...}", item)
		}
		max, ok := item[1].(int64)
		if !ok || max < 0 || max > HashSlots {
			return nil, fmt.Errorf("got %v, expected {int64, int64, string...}", item)
		}

		info := ClusterSlotInfo{int(min), int(max), make([]string, len(item)-2)}
		for n, ipair := range item[2:] {
			pair, ok := ipair.([]interface{})
			if !ok || len(pair) != 2 {
				return nil, fmt.Errorf("got %v, expected []interface{host, port}", viface)
			}

			ip, ok := pair[0].(string)
			if !ok || len(ip) < 1 {
				return nil, fmt.Errorf("got %v, expected IP PORT pair", pair)
			}
			port, ok := pair[1].(int64)
			if !ok || port < 1 {
				return nil, fmt.Errorf("got %v, expected IP PORT pair", pair)
			}

			info.Addrs[n] = net.JoinHostPort(ip, strconv.FormatInt(port, 10))
		}
		infos = append(infos, info)
	}
	return infos, nil
}

//------------------------------------------------------------------------------

// HashSlot returns a consistent slot number between 0 and 16383
// for any given string key
func HashSlot(key string) int {
	if s := strings.IndexByte(key, '{'); s > -1 {
		if e := strings.IndexByte(key[s+1:], '}'); e > 0 {
			key = key[s+1 : s+e+1]
		}
	}
	if key == "" {
		return rand.Intn(HashSlots)
	}
	return int(crc16sum(key)) % HashSlots
}

// CRC16 implementation according to CCITT standards.
// Copyright 2001-2010 Georges Menie (www.menie.org)
// Copyright 2013 The Go Authors. All rights reserved.
var crc16tab = [256]uint16{
	0x0000, 0x1021, 0x2042, 0x3063, 0x4084, 0x50a5, 0x60c6, 0x70e7,
	0x8108, 0x9129, 0xa14a, 0xb16b, 0xc18c, 0xd1ad, 0xe1ce, 0xf1ef,
	0x1231, 0x0210, 0x3273, 0x2252, 0x52b5, 0x4294, 0x72f7, 0x62d6,
	0x9339, 0x8318, 0xb37b, 0xa35a, 0xd3bd, 0xc39c, 0xf3ff, 0xe3de,
	0x2462, 0x3443, 0x0420, 0x1401, 0x64e6, 0x74c7, 0x44a4, 0x5485,
	0xa56a, 0xb54b, 0x8528, 0x9509, 0xe5ee, 0xf5cf, 0xc5ac, 0xd58d,
	0x3653, 0x2672, 0x1611, 0x0630, 0x76d7, 0x66f6, 0x5695, 0x46b4,
	0xb75b, 0xa77a, 0x9719, 0x8738, 0xf7df, 0xe7fe, 0xd79d, 0xc7bc,
	0x48c4, 0x58e5, 0x6886, 0x78a7, 0x0840, 0x1861, 0x2802, 0x3823,
	0xc9cc, 0xd9ed, 0xe98e, 0xf9af, 0x8948, 0x9969, 0xa90a, 0xb92b,
	0x5af5, 0x4ad4, 0x7ab7, 0x6a96, 0x1a71, 0x0a50, 0x3a33, 0x2a12,
	0xdbfd, 0xcbdc, 0xfbbf, 0xeb9e, 0x9b79, 0x8b58, 0xbb3b, 0xab1a,
	0x6ca6, 0x7c87, 0x4ce4, 0x5cc5, 0x2c22, 0x3c03, 0x0c60, 0x1c41,
	0xedae, 0xfd8f, 0xcdec, 0xddcd, 0xad2a, 0xbd0b, 0x8d68, 0x9d49,
	0x7e97, 0x6eb6, 0x5ed5, 0x4ef4, 0x3e13, 0x2e32, 0x1e51, 0x0e70,
	0xff9f, 0xefbe, 0xdfdd, 0xcffc, 0xbf1b, 0xaf3a, 0x9f59, 0x8f78,
	0x9188, 0x81a9, 0xb1ca, 0xa1eb, 0xd10c, 0xc12d, 0xf14e, 0xe16f,
	0x1080, 0x00a1, 0x30c2, 0x20e3, 0x5004, 0x4025, 0x7046, 0x6067,
	0x83b9, 0x9398, 0xa3fb, 0xb3da, 0xc33d, 0xd31c, 0xe37f, 0xf35e,
	0x02b1, 0x1290, 0x22f3, 0x32d2, 0x4235, 0x5214, 0x6277, 0x7256,
	0xb5ea, 0xa5cb, 0x95a8, 0x8589, 0xf56e, 0xe54f, 0xd52c, 0xc50d,
	0x34e2, 0x24c3, 0x14a0, 0x0481, 0x7466, 0x6447, 0x5424, 0x4405,
	0xa7db, 0xb7fa, 0x8799, 0x97b8, 0xe75f, 0xf77e, 0xc71d, 0xd73c,
	0x26d3, 0x36f2, 0x0691, 0x16b0, 0x6657, 0x7676, 0x4615, 0x5634,
	0xd94c, 0xc96d, 0xf90e, 0xe92f, 0x99c8, 0x89e9, 0xb98a, 0xa9ab,
	0x5844, 0x4865, 0x7806, 0x6827, 0x18c0, 0x08e1, 0x3882, 0x28a3,
	0xcb7d, 0xdb5c, 0xeb3f, 0xfb1e, 0x8bf9, 0x9bd8, 0xabbb, 0xbb9a,
	0x4a75, 0x5a54, 0x6a37, 0x7a16, 0x0af1, 0x1ad0, 0x2ab3, 0x3a92,
	0xfd2e, 0xed0f, 0xdd6c, 0xcd4d, 0xbdaa, 0xad8b, 0x9de8, 0x8dc9,
	0x7c26, 0x6c07, 0x5c64, 0x4c45, 0x3ca2, 0x2c83, 0x1ce0, 0x0cc1,
	0xef1f, 0xff3e, 0xcf5d, 0xdf7c, 0xaf9b, 0xbfba, 0x8fd9, 0x9ff8,
	0x6e17, 0x7e36, 0x4e55, 0x5e74, 0x2e93, 0x3eb2, 0x0ed1, 0x1ef0,
}

func crc16sum(key string) (crc uint16) {
	for _, v := range key {
		crc = (crc << 8) ^ crc16tab[(byte(crc>>8)^byte(v))&0x00ff]
	}
	return
}
