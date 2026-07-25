package redisadapter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestDispatchersPreserveTypedRoutesAndResultEnvelopes(t *testing.T) {
	tests := []struct {
		name        string
		call        redistoolset.Call
		wantCommand []string
		wantData    map[string]any
	}{
		{"list read", readCall("redis_list", "length", map[string]any{"key": "list"}), []string{"llen", "list"}, map[string]any{"count": int64(3)}},
		{"set read", readCall("redis_set", "cardinality", map[string]any{"key": "set"}), []string{"scard", "set"}, map[string]any{"count": int64(2)}},
		{"sorted set read", readCall("redis_sorted_set", "cardinality", map[string]any{"key": "sorted"}), []string{"zcard", "sorted"}, map[string]any{"count": int64(4)}},
		{"stream read", readCall("redis_stream", "length", map[string]any{"key": "stream"}), []string{"xlen", "stream"}, map[string]any{"count": int64(6)}},
		{"key write", writeCall("redis_key_write", "persist_expiry", map[string]any{"key": "key"}), []string{"persist", "key"}, mutation("applied", true)},
		{"string write", writeCall("redis_string_write", "append", map[string]any{"key": "key", "value": binary("value")}), []string{"append", "key", "value"}, mutation("length", int64(5))},
		{"hash write", writeCall("redis_hash_write", "delete_fields", map[string]any{"key": "hash", "fields": []any{binary("field")}}), []string{"hdel", "hash", "field"}, mutation("deleted_count", int64(1))},
		{"list write", writeCall("redis_list_write", "trim", map[string]any{"key": "list", "start": float64(0), "stop": float64(0)}), []string{"ltrim", "list", "0", "0"}, mutation("applied", true)},
		{"set write", writeCall("redis_set_write", "add", map[string]any{"key": "set", "members": []any{binary("member")}}), []string{"sadd", "set", "member"}, mutation("added_count", int64(1))},
		{"sorted set write", writeCall("redis_sorted_set_write", "increment", map[string]any{"key": "sorted", "increment": float64(2.5), "member": binary("member")}), []string{"zincrby", "sorted", "2.5", "member"}, map[string]any{"scalar": float64(2.5)}},
		{"stream write", writeCall("redis_stream_write", "group", map[string]any{"action": "destroy", "key": "stream", "group": "group"}), []string{"xgroup", "destroy", "stream", "group"}, map[string]any{"mutation": map[string]any{"applied": true, "count": int64(1)}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newDispatcherRESPServer(t)
			adapter, err := New(config.RedisConfig{Topology: "standalone", Addresses: []string{server.listener.Addr().String()}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = adapter.Close() })

			data, err := adapter.Call(context.Background(), test.call)
			if err != nil {
				t.Fatalf("Call: %v", err)
			}
			if !reflect.DeepEqual(data, test.wantData) {
				t.Errorf("result envelope = %#v, want %#v", data, test.wantData)
			}
			if command := server.command(t, test.wantCommand[0]); !reflect.DeepEqual(command, test.wantCommand) {
				t.Errorf("Redis command = %q, want %q", command, test.wantCommand)
			}
		})
	}
}

func TestDispatchersRejectUnknownToolsWithoutAClient(t *testing.T) {
	adapter := &Adapter{}
	tests := []struct {
		name    string
		execute func(context.Context, redistoolset.Call) (map[string]any, error)
	}{
		{"collection read", adapter.executeCollectionRead},
		{"key and string write", adapter.executeKeyAndStringWrite},
		{"other write", adapter.executeOtherWrite},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.execute(context.Background(), redistoolset.Call{Tool: "unknown_tool", Operation: "unknown_operation"})
			var callErr *redistoolset.CallError
			if !errors.As(err, &callErr) || callErr.Category != "topology_unsupported" {
				t.Fatalf("error = %v, want topology_unsupported CallError", err)
			}
		})
	}
}

func readCall(tool, operation string, args map[string]any) redistoolset.Call {
	return redistoolset.Call{Tool: tool, Operation: operation, Args: args}
}

func writeCall(tool, operation string, args map[string]any) redistoolset.Call {
	return redistoolset.Call{Tool: tool, Operation: operation, Args: args, Write: true}
}

func binary(value string) map[string]any {
	return map[string]any{"utf8": value}
}

type dispatcherRESPServer struct {
	listener net.Listener
	commands chan []string
}

func newDispatcherRESPServer(t *testing.T) *dispatcherRESPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &dispatcherRESPServer{listener: listener, commands: make(chan []string, 32)}
	go server.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *dispatcherRESPServer) serve() {
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(connection)
	}
}

func (s *dispatcherRESPServer) handle(connection net.Conn) {
	defer func() { _ = connection.Close() }()
	reader, writer := bufio.NewReader(connection), bufio.NewWriter(connection)
	for {
		command, err := readDispatcherRESPCommand(reader)
		if err != nil {
			return
		}
		s.commands <- command
		if _, err := writer.WriteString(dispatcherRESPReply(command)); err != nil {
			return
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}

func (s *dispatcherRESPServer) command(t *testing.T, name string) []string {
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

func readDispatcherRESPCommand(reader *bufio.Reader) ([]string, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if prefix != '*' {
		return nil, fmt.Errorf("expected RESP array, got %q", prefix)
	}
	count, err := readDispatcherRESPCount(reader)
	if err != nil {
		return nil, err
	}
	command := make([]string, count)
	for index := range command {
		if prefix, err = reader.ReadByte(); err != nil {
			return nil, err
		} else if prefix != '$' {
			return nil, fmt.Errorf("expected RESP bulk string, got %q", prefix)
		}
		length, err := readDispatcherRESPCount(reader)
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

func readDispatcherRESPCount(reader *bufio.Reader) (int, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(line))
}

func dispatcherRESPReply(command []string) string {
	if len(command) == 0 {
		return "+OK\r\n"
	}
	switch strings.ToLower(command[0]) {
	case "hello":
		return "%2\r\n+server\r\n+redis\r\n+version\r\n+7.0.0\r\n"
	case "llen":
		return ":3\r\n"
	case "scard":
		return ":2\r\n"
	case "zcard":
		return ":4\r\n"
	case "xlen":
		return ":6\r\n"
	case "persist", "hdel", "sadd", "xgroup":
		return ":1\r\n"
	case "append":
		return ":5\r\n"
	case "ltrim":
		return "+OK\r\n"
	case "zincrby":
		return "$3\r\n2.5\r\n"
	default:
		return "+OK\r\n"
	}
}
