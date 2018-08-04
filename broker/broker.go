package broker

import (
	"context"
	"fmt"
	"github.com/breathenetwork/chat/api"
	"github.com/breathenetwork/chat/logger"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
	"io"
	"net"
	"strconv"
	"time"
)

const (
	SSHStatePrefix = iota
	SSHStateVersion
	SSHStateComment
	SSHStateLFVersion
	SSHStateLFComment
)

type ConnReader struct {
	Conn   net.Conn
	Reader io.Reader
}

func (r *ConnReader) Read(b []byte) (int, error) {
	return r.Reader.Read(b)
}

func (r *ConnReader) Write(b []byte) (int, error) {
	return r.Conn.Write(b)
}

func (r *ConnReader) Close() error {
	return nil
}

func (r *ConnReader) LocalAddr() net.Addr {
	return r.Conn.LocalAddr()
}

func (r *ConnReader) RemoteAddr() net.Addr {
	return r.Conn.LocalAddr()
}

func (r *ConnReader) SetDeadline(t time.Time) error {
	return r.Conn.SetDeadline(t)
}

func (r *ConnReader) SetReadDeadline(t time.Time) error {
	return r.Conn.SetReadDeadline(t)
}

func (r *ConnReader) SetWriteDeadline(t time.Time) error {
	return r.Conn.SetWriteDeadline(t)
}

func NewConnReader(conn net.Conn, r io.Reader) net.Conn {
	return &ConnReader{
		Conn:   conn,
		Reader: r,
	}
}

type ServerEventType uint32

type DisconnectFunc func()

type ServerEvent interface {
}

type ServerEventDisconnected struct {
	Id string
}

type ServerEventConnected struct {
	Id         string
	Ip         string
	Port       uint32
	Disconnect DisconnectFunc
}

type ServerEventData struct {
	Id   string
	Data []byte
}

type SendFunc func(data []byte)

type ServerEventAuth struct {
	Id    string
	Type  api.AuthType
	Data  []byte
	Reply chan bool
	Send  SendFunc
}

type ServerEventBanner struct {
	Id    string
	Reply chan string
}

type ServerEventBackendInit struct {
	Breathe api.BreatheServerClient
	Done    chan struct{}
}

type ServerEventDisconnect struct {
	Id string
}

type ServerEventSend struct {
	Id   string
	Data []byte
}

type Client struct {
	Id         string
	Ip         string
	Port       uint32
	Disconnect DisconnectFunc
	Send       SendFunc
	Type       api.AuthType
	Data       []byte
	Authed     bool
}

type Server struct {
	Id          string
	ListenAddr  string
	BackendAddr string
	Control     chan ServerEvent
	Signer      ssh.Signer
	Queue       []ServerEvent
	Breathe     api.BreatheServerClient
	Clients     map[string]*Client
	Contexts    []context.CancelFunc
}

type Handler interface {
	Recv(data []byte) Handler
	Send(data []byte)
}

type UnsureHandler struct {
	Id       string
	Server   *Server
	Conn     net.Conn
	QueueIn  [][]byte
	QueueOut [][]byte
	Probe    []byte
	Timer    *time.Timer
}

func (handler *UnsureHandler) Recv(data []byte) Handler {
	handler.QueueIn = append(handler.QueueIn, data)
	handler.Probe = append(handler.Probe, data...)
	state := SSHStatePrefix
	for i, c := range handler.Probe {
		if i > 255 {
			handler.Timer.Stop()
			return NewPlainHandler(handler.Conn, handler.QueueIn, handler.QueueOut, handler.Server, handler.Id)
		}
		switch state {
		case SSHStatePrefix:
			if "SSH-"[i] != c {
				handler.Timer.Stop()
				return NewPlainHandler(handler.Conn, handler.QueueIn, handler.QueueOut, handler.Server, handler.Id)
			}
			if i == 3 {
				state = SSHStateVersion
			}
		case SSHStateVersion:
			if c == ' ' {
				state = SSHStateComment
			} else if c == '\r' {
				state = SSHStateLFVersion
			} else if c < '!' || c > '~' {
				handler.Timer.Stop()
				return NewPlainHandler(handler.Conn, handler.QueueIn, handler.QueueOut, handler.Server, handler.Id)
			}
		case SSHStateComment:
			if c == '\r' {
				state = SSHStateLFComment
			}
		case SSHStateLFVersion:
			if c == '\n' {
				handler.Timer.Reset(5 * time.Second)
				return NewSshHandler(handler.Conn, handler.Timer, handler.QueueIn, handler.QueueOut, handler.Server, handler.Id)
			} else {
				handler.Timer.Stop()
				return NewPlainHandler(handler.Conn, handler.QueueIn, handler.QueueOut, handler.Server, handler.Id)
			}
		case SSHStateLFComment:
			if c == '\n' {
				handler.Timer.Reset(5 * time.Second)
				return NewSshHandler(handler.Conn, handler.Timer, handler.QueueIn, handler.QueueOut, handler.Server, handler.Id)
			} else {
				state = SSHStateComment
			}
		}
	}
	return handler
}

func (handler *UnsureHandler) Send(data []byte) {
	handler.QueueOut = append(handler.QueueOut, data)
}

func NewUnsureHandler(conn net.Conn, timer *time.Timer, server *Server, id string) Handler {
	return &UnsureHandler{
		Id:     id,
		Server: server,
		Conn:   conn,
		Timer:  timer,
	}
}

type PlainHandler struct {
	Id     string
	Server *Server
	Conn   net.Conn
}

func (handler *PlainHandler) Recv(data []byte) Handler {
	handler.Server.Control <- &ServerEventData{
		Id:   handler.Id,
		Data: data,
	}
	return handler
}

func (handler *PlainHandler) Send(data []byte) {
	handler.Conn.Write(data)
}

func NewPlainHandler(conn net.Conn, in [][]byte, out [][]byte, server *Server, id string) Handler {
	s := &PlainHandler{
		Id:     id,
		Server: server,
		Conn:   conn,
	}

	for _, buf := range in {
		s.Recv(buf)
	}

	for _, buf := range out {
		conn.Write(buf)
	}

	rep := make(chan bool)
	server.Control <- &ServerEventAuth{
		Id:    id,
		Type:  api.AuthType_IDENTITY_AUTH_TYPE,
		Data:  nil,
		Reply: rep,
		Send:  s.Send,
	}
	res := <-rep
	if !res {
		server.Control <- &ServerEventDisconnect{
			Id: id,
		}
	}

	return s
}

type SshHandler struct {
	Id       string
	Server   *Server
	Conn     net.Conn
	QueueIn  [][]byte
	QueueOut [][]byte
	Writer   io.Writer
	Channel  ssh.Channel
	Error    error
	Timer    *time.Timer
}

func (handler *SshHandler) Recv(data []byte) Handler {
	if handler.Error != nil {
		return NewPlainHandler(handler.Conn, handler.QueueIn, handler.QueueOut, handler.Server, handler.Id)
	}
	if handler.Channel == nil {
		handler.QueueIn = append(handler.QueueIn, data)
	}
	handler.Writer.Write(data)
	return handler
}

func (handler *SshHandler) Send(data []byte) {
	if handler.Channel != nil {
		handler.Channel.Write(data)
	} else {
		handler.QueueOut = append(handler.QueueOut, data)
	}
}

func NewSshHandler(conn net.Conn, timer *time.Timer, in [][]byte, out [][]byte, server *Server, id string) Handler {
	reader, writer := io.Pipe()
	rconn := NewConnReader(conn, reader)

	s := &SshHandler{
		Id:       id,
		Server:   server,
		Conn:     conn,
		QueueIn:  in,
		QueueOut: out,
		Writer:   writer,
		Timer:    timer,
	}

	config := &ssh.ServerConfig{
		NoClientAuth: false,
		MaxAuthTries: 3,
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			rep := make(chan bool)
			server.Control <- &ServerEventAuth{
				Id:    id,
				Type:  api.AuthType_PUBKEY_AUTH_TYPE,
				Data:  key.Marshal(),
				Reply: rep,
				Send:  s.Send,
			}

			reply := <-rep
			close(rep)
			if reply {
				return &ssh.Permissions{
					CriticalOptions: make(map[string]string),
					Extensions:      make(map[string]string),
				}, nil
			} else {
				return nil, ssh.ErrNoAuth
			}
		},
		ServerVersion: "SSH-2.0-breathe.network",
		BannerCallback: func(conn ssh.ConnMetadata) string {
			rep := make(chan string)
			server.Control <- &ServerEventBanner{
				Id:    id,
				Reply: rep,
			}
			reply := <-rep
			close(rep)
			return reply
		},
	}
	config.AddHostKey(server.Signer)

	go func() {
		if sshconn, newchan, newreq, err := ssh.NewServerConn(rconn, config); err != nil {
			s.Error = err
			s.Timer.Stop()
		} else {
			s.Timer.Stop()
			defer sshconn.Close()
			go ssh.DiscardRequests(newreq)
			chreq := <-newchan
			if ch, req, err := chreq.Accept(); err != nil {
				sshconn.Close()
				return
			} else {
				s.Channel = ch
				s.QueueIn = nil
				go func() {
					for r := range req {
						switch r.Type {
						case "shell":
							r.Reply(true, nil)
						default:
							r.Reply(false, nil)
						}
					}
				}()
				go func() {
					buf := make([]byte, 2048)
					for {
						n, err := ch.Read(buf)
						if err != nil {
							sshconn.Close()
							return
						}
						server.Control <- &ServerEventData{
							Id:   id,
							Data: buf[:n],
						}
					}
				}()
			}
			for chreq := range newchan {
				err := chreq.Reject(ssh.ResourceShortage, "Only one channel allowed")
				if err != nil {
					sshconn.Close()
					return
				}
				continue
			}
		}
	}()

	for _, buf := range in {
		s.Recv(buf)
	}

	return s
}

func (server *Server) Read(conn net.Conn, id string) {
	addr := conn.RemoteAddr().(*net.TCPAddr)

	rcv := make(chan []byte)

	closed := false
	server.Control <- &ServerEventConnected{
		Id:   id,
		Ip:   addr.IP.String(),
		Port: uint32(addr.Port),
		Disconnect: func() {
			closed = true
			conn.Close()
		},
	}

	go func() {
		timer := time.NewTimer(250 * time.Millisecond)
		handler := NewUnsureHandler(conn, timer, server, id)
		for {
			select {
			case buf := <-rcv:
				if len(buf) == 0 {
					return
				}
				handler = handler.Recv(buf)
			case <-timer.C:
				switch t := handler.(type) {
				case *UnsureHandler:
					handler = NewPlainHandler(conn, t.QueueIn, t.QueueOut, server, id)
				case *SshHandler:
					handler = NewPlainHandler(conn, t.QueueIn, t.QueueOut, server, id)
				}
			}
		}
	}()

	buf := make([]byte, 2048)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		data := make([]byte, n)
		copy(data, buf)
		rcv <- data
	}

	close(rcv)
	server.Control <- &ServerEventDisconnected{
		Id: id,
	}
}

func (server *Server) BreatheBackend() {
	breathe := (api.BreatheServerClient)(nil)
	if client, err := grpc.Dial(server.BackendAddr, grpc.WithInsecure(), grpc.WithDialer(func(s string, duration time.Duration) (net.Conn, error) {
		ch := make(chan struct{})
		server.Control <- &ServerEventBackendInit{
			Breathe: breathe,
			Done:    ch,
		}
		for {
			conn, err := net.Dial("tcp", s)
			if err == nil {
				close(ch)
				return conn, nil
			}
		}
	})); err != nil {
		logger.Error(err)
	} else {
		breathe = api.NewBreatheServerClient(client)
	}
}

func (server *Server) ProcessControlEvent(raw ServerEvent) {
	switch event := raw.(type) {
	case *ServerEventConnected:
		if server.Breathe == nil {
			server.Queue = append(server.Queue, event)
		} else {
			client := &Client{
				Id:         event.Id,
				Ip:         event.Ip,
				Port:       event.Port,
				Disconnect: event.Disconnect,
			}
			if resp, err := server.Breathe.ClientConnected(context.Background(), &api.ClientConnectedEvent{
				Id:   event.Id,
				Ip:   event.Ip,
				Port: event.Port,
			}); err != nil {
				logger.Error(err)
			} else if resp.Status != api.Status_PERMIT {
				client.Disconnect()
			} else {
				server.Clients[event.Id] = client
				logger.Info("conn", event.Id, event.Ip, event.Port)
			}
		}
	case *ServerEventDisconnected:
		if server.Breathe == nil {
			server.Queue = append(server.Queue, event)
		} else {
			server.Breathe.ClientDisconnect(context.Background(), &api.ClientDisconnectedEvent{
				Id: event.Id,
			})
			delete(server.Clients, event.Id)
			logger.Info("disc", event.Id)
		}
	case *ServerEventData:
		client, ok := server.Clients[event.Id]
		fmt.Println(ok, client)
		if server.Breathe == nil || (ok && !client.Authed) {
			server.Queue = append(server.Queue, event)
		} else {
			if ok {
				if resp, err := server.Breathe.ClientData(context.Background(), &api.ClientDataEvent{
					Id:   event.Id,
					Data: event.Data,
				}); err != nil {
					logger.Error(err)
				} else if resp.Status != api.Status_PERMIT {
					client.Disconnect()
				}
			}
		}
	case *ServerEventAuth:
		if server.Breathe == nil {
			server.Queue = append(server.Queue, event)
		} else {
			client, ok := server.Clients[event.Id]
			if ok {
				logger.Info("auth", event.Id, event.Type)
				client.Type = event.Type
				client.Data = event.Data
				client.Send = event.Send
				if resp, err := server.Breathe.ClientAuth(context.Background(), &api.ClientAuthEvent{
					Id:   event.Id,
					Type: event.Type,
					Data: event.Data,
				}); err != nil {
					logger.Error(err)
				} else if resp.Status != api.Status_PERMIT {
					event.Reply <- false
				} else {
					client.Authed = true
					event.Reply <- true
				}
			}
		}
	case *ServerEventBanner:
		if server.Breathe == nil {
			server.Queue = append(server.Queue, event)
		} else {
			logger.Info("bann", event.Id)
			if resp, err := server.Breathe.ClientBanner(context.Background(), &api.ClientBannerRequest{}); err != nil {
				logger.Error(err)
				event.Reply <- ""
			} else {
				event.Reply <- resp.Banner
			}
		}
	case *ServerEventBackendInit:
		logger.Info("connected!")
		clients := make([]*api.ConnectedClient, 0)
		for _, client := range server.Clients {
			clients = append(clients, &api.ConnectedClient{
				Id:       client.Id,
				Ip:       client.Ip,
				Port:     client.Port,
				AuthType: client.Type,
				AuthData: client.Data,
			})
		}
		if _, err := server.Breathe.Bootstrap(context.Background(), &api.BootstrapEvent{
			Clients: clients,
		}); err != nil {
			logger.Error(err)
		}
		go server.ProcessCommands()
	case *ServerEventDisconnect:
		logger.Info("CMD: disconnect", event.Id)
		if client, ok := server.Clients[event.Id]; ok {
			client.Disconnect()
		}
	case *ServerEventSend:
		if client, ok := server.Clients[event.Id]; ok {
			client.Send(event.Data)
		}
	}
}

func (server *Server) ProcessCommands() {
	if server.Breathe != nil {
		if stream, err := server.Breathe.CommandStream(context.Background(), &api.CommandStreamRequest{}); err != nil {
			logger.Error(err)
		} else {
			for {
				if cmd, err := stream.Recv(); err != nil {
					logger.Error(err)
					return
				} else {
					switch cmd.Type {
					case api.CommandType_DISCONNECT_USER:
						req := cmd.Data.(*api.Command_DisconnectUser).DisconnectUser
						server.Control <- &ServerEventDisconnect{
							Id: req.Id,
						}
					case api.CommandType_SEND_USER:
						req := cmd.Data.(*api.Command_SendUser).SendUser
						server.Control <- &ServerEventSend{
							Id:   req.Id,
							Data: req.Data,
						}
					}
				}
			}
		}
	}
}

func (server *Server) ProcessControl() {
	for raw := range server.Control {
		switch event := raw.(type) {
		case *ServerEventBackendInit:
			server.Breathe = nil
			logger.Info("backend connecting...")
			<-event.Done
			server.Breathe = event.Breathe

			server.ProcessControlEvent(raw)

			for _, old := range server.Queue {
				server.ProcessControlEvent(old)
			}

			server.Queue = nil
		default:
			server.ProcessControlEvent(raw)
		}
	}
}

func (server *Server) Serve() {
	go server.BreatheBackend()
	go server.ProcessControl()
	glob := uint64(0)
	for {
		gstr := strconv.FormatUint(glob, 10)
		if l, err := net.Listen("tcp", server.ListenAddr); err != nil {
			logger.Error(err)
		} else {
			loc := uint64(0)
			for {
				lstr := strconv.FormatUint(loc, 10)
				if c, err := l.Accept(); err != nil {
					logger.Error(err)
					break
				} else {
					tim := strconv.FormatInt(time.Now().Unix(), 10)
					go server.Read(c, tim+"-"+gstr+"-"+lstr)
				}
				loc++
			}
		}
		glob++
	}
}

func NewServer(id string, listen string, backend string, signer ssh.Signer) *Server {
	return &Server{
		Id:          id,
		ListenAddr:  listen,
		BackendAddr: backend,
		Control:     make(chan ServerEvent),
		Signer:      signer,
		Clients:     make(map[string]*Client),
	}
}
