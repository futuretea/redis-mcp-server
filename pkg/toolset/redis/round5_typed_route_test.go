package redis_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/futuretea/redis-mcp-server/internal/redisadapter"
	"github.com/futuretea/redis-mcp-server/pkg/core/config"
	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestTypedAdapterPreservesRoundFiveOptionalDefaults(t *testing.T) {
	testCases := []struct {
		name string
		call redistoolset.Call
		want []string
	}{
		{
			name: "decrement omitted amount",
			call: redistoolset.Call{Tool: "redis_string_write", Operation: "decrement", Write: true, Args: map[string]any{"key": "key"}},
			want: []string{"decrby", "key", "1"},
		},
		{
			name: "decrement explicit zero amount",
			call: redistoolset.Call{Tool: "redis_string_write", Operation: "decrement", Write: true, Args: map[string]any{"key": "key", "amount": float64(0)}},
			want: []string{"decrby", "key", "0"},
		},
		{
			name: "list pop many omitted count",
			call: redistoolset.Call{Tool: "redis_list_write", Operation: "pop_many", Write: true, Args: map[string]any{"keys": []any{"list"}, "side": "left"}},
			want: []string{"lmpop", "1", "list", "left", "count", "1"},
		},
		{
			name: "sorted set pop omitted count",
			call: redistoolset.Call{Tool: "redis_sorted_set_write", Operation: "pop", Write: true, Args: map[string]any{"keys": []any{"zset"}, "side": "min"}},
			want: []string{"zmpop", "1", "zset", "min", "count", "1"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newRedisRESPServer(t)
			adapter, err := redisadapter.New(config.RedisConfig{Topology: "standalone", Addresses: []string{server.listener.Addr().String()}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = adapter.Close() })

			if _, err := adapter.Call(context.Background(), testCase.call); err != nil {
				t.Fatalf("Call: %v", err)
			}
			if got := server.command(t, testCase.want[0]); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Redis command = %q, want %q", got, testCase.want)
			}
		})
	}
}

type redisRESPServer struct {
	listener net.Listener
	commands chan []string
	reply    func([]string) string
}

func newRedisRESPServer(t *testing.T) *redisRESPServer {
	return newRedisRESPServerWithReply(t, redisRESPReply)
}

func newRedisRESPServerWithReply(t *testing.T, reply func([]string) string) *redisRESPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &redisRESPServer{listener: listener, commands: make(chan []string, 16), reply: reply}
	go server.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *redisRESPServer) serve() {
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(connection)
	}
}

func (s *redisRESPServer) handle(connection net.Conn) {
	defer func() { _ = connection.Close() }()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	for {
		command, err := readRESPCommand(reader)
		if err != nil {
			return
		}
		s.commands <- command
		if _, err := writer.WriteString(s.reply(command)); err != nil {
			return
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}

func (s *redisRESPServer) command(t *testing.T, name string) []string {
	t.Helper()
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
	for {
		select {
		case command := <-s.commands:
			if len(command) > 0 && strings.EqualFold(command[0], name) {
				return command
			}
		case <-timeout.C:
			t.Fatalf("did not receive %s command", name)
		}
	}
}

func readRESPCommand(reader *bufio.Reader) ([]string, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if prefix != '*' {
		return nil, fmt.Errorf("expected RESP array, got %q", prefix)
	}
	count, err := readRESPCount(reader)
	if err != nil {
		return nil, err
	}
	command := make([]string, count)
	for index := range command {
		prefix, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if prefix != '$' {
			return nil, fmt.Errorf("expected RESP bulk string, got %q", prefix)
		}
		length, err := readRESPCount(reader)
		if err != nil {
			return nil, err
		}
		payload := make([]byte, length+2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		command[index] = string(payload[:length])
	}
	return command, nil
}

func readRESPCount(reader *bufio.Reader) (int, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"))
}

func redisRESPReply(command []string) string {
	if len(command) == 0 {
		return "+OK\r\n"
	}
	switch strings.ToLower(command[0]) {
	case "hello":
		return "%2\r\n+server\r\n+redis\r\n+version\r\n+7.0.0\r\n"
	case "decrby":
		return ":0\r\n"
	case "lmpop":
		return "*2\r\n$4\r\nlist\r\n*1\r\n$1\r\nv\r\n"
	case "zmpop":
		return "*2\r\n$4\r\nzset\r\n*1\r\n*2\r\n$1\r\nm\r\n$1\r\n1\r\n"
	case "zcount", "zlexcount", "delex":
		return ":1\r\n"
	case "digest":
		return "$16\r\nffffffffffffffff\r\n"
	default:
		return "+OK\r\n"
	}
}
