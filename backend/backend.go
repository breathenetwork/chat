package backend

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"github.com/breathenetwork/chat/api"
	"github.com/breathenetwork/chat/logger"
	"github.com/go-pg/pg"
	"github.com/go-pg/pg/orm"
	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	MessageSizeLimit = 1024
	NameMinSize      = 3
	NameMaxSize      = 32
)

const (
	ArgText = iota
	ArgQuote
	ArgSpace
	ArgEscape
)

const (
	DefaultDomain   = "breathe.network"
	DefaultName     = "anonymous"
	DefaultEndlines = "\n"
	DefaultCredit   = 30
)

var DefaultEncoding = unicode.UTF8

type Handler func(server *Server, clinet *Client, message *Message, args []string)

type UserEntity struct {
	Id          uint64
	Name        string
	Encoding    string
	Endlines    string
	Email       string
	EmailPublic bool
	Signup      time.Time
	Lastseen    time.Time
	Credit      uint64
	Admin       bool
}

type AuthEntity struct {
	Id      uint64
	Type    api.AuthType
	Data    string
	Default uint64
}

type AuthUserEntity struct {
	UserId uint64
	AuthId uint64
}

type LoginEntity struct {
	Id   uint64
	User uint64
	Ip   string
	Port uint32
	Time time.Time
}

type MemoEntity struct {
	Id       uint64
	Sender   uint64
	Receiver uint64
	Body     string
	Time     time.Time
}

type MessageEntity struct {
	Id         uint64
	SenderId   uint64
	SenderIp   string
	SenderPort uint32
	Body       string
	Time       time.Time
	Action     bool
}

type BansEntity struct {
	Id         uint64
	Expression string
	Reason     string
	Time       time.Time
}

type Message struct {
	Sender     UserSettings
	SenderId   uint64
	SenderIp   string
	SenderPort uint32
	Body       string
	Time       time.Time
	Action     bool
	Count      uint64
	Transient  bool
	History    bool
}

type UserSettings struct {
	Id          uint64
	Name        string
	Encoding    encoding.Encoding
	Endlines    string
	Email       string
	EmailPublic bool
	Signup      time.Time
	Lastseen    time.Time
	Credit      uint64
	Admin       bool
}

type Client struct {
	Id         string
	Ip         string
	Port       uint32
	AuthEntity AuthEntity
	Settings   UserSettings
	Queue      []*Message
	Buffer     []byte
	Throttle   *Throttle
}

type ServerEvent interface {
}

type ServerEventCommand struct {
	Stream api.BreatheServer_CommandStreamServer
}

type ServerEventBootstrap struct {
	Clients []*api.ConnectedClient
}

type ServerEventConnected struct {
	Id     string
	Ip     string
	Port   uint32
	Status chan bool
}

type ServerEventDisconnected struct {
	Id string
}

type ServerEventData struct {
	Id     string
	Data   []byte
	Status chan bool
}

type ServerEventAuth struct {
	Id     string
	Type   api.AuthType
	Data   []byte
	Status chan bool
}

type ServerEventBanner struct {
	Result chan string
}

type Throttle struct {
	Count    uint64
	LastUsed time.Time
}

type Server struct {
	DB         *pg.DB
	Control    chan ServerEvent
	Stream     api.BreatheServer_CommandStreamServer
	Clients    map[string]*Client
	ServerUser *Client
	Queue      []ServerEvent
	Throttle   map[string]*Throttle
}

func (server *Server) CommandStream(req *api.CommandStreamRequest, stream api.BreatheServer_CommandStreamServer) error {
	if req == nil {
		return nil
	}
	server.Control <- &ServerEventCommand{
		Stream: stream,
	}
	<-stream.Context().Done()
	server.Control <- &ServerEventCommand{
		Stream: nil,
	}
	return nil
}

func (server *Server) Bootstrap(ctx context.Context, req *api.BootstrapEvent) (*api.Response, error) {
	if req == nil {
		return nil, api.NilRequestError
	}
	server.Control <- &ServerEventBootstrap{
		Clients: req.Clients,
	}
	return &api.Response{
		Status: api.Status_PERMIT,
	}, nil
}

func (server *Server) ClientConnected(ctx context.Context, req *api.ClientConnectedEvent) (*api.Response, error) {
	if req == nil {
		return nil, api.NilRequestError
	}

	status := make(chan bool)
	server.Control <- &ServerEventConnected{
		Id:     req.Id,
		Ip:     req.Ip,
		Port:   req.Port,
		Status: status,
	}

	res := <-status
	close(status)
	if res {
		return &api.Response{
			Status: api.Status_PERMIT,
		}, nil
	} else {
		return &api.Response{
			Status: api.Status_DENY,
		}, nil
	}

}

func (server *Server) ClientDisconnect(ctx context.Context, req *api.ClientDisconnectedEvent) (*api.Response, error) {
	if req == nil {
		return nil, api.NilRequestError
	}
	server.Control <- &ServerEventDisconnected{
		Id: req.Id,
	}
	return &api.Response{
		Status: api.Status_PERMIT,
	}, nil
}

func (server *Server) ClientData(ctx context.Context, req *api.ClientDataEvent) (*api.Response, error) {
	if req == nil {
		return nil, api.NilRequestError
	}
	status := make(chan bool)
	server.Control <- &ServerEventData{
		Id:     req.Id,
		Data:   req.Data,
		Status: status,
	}

	res := <-status
	close(status)
	if res {
		return &api.Response{
			Status: api.Status_PERMIT,
		}, nil
	} else {
		return &api.Response{
			Status: api.Status_DENY,
		}, nil
	}
}

func (server *Server) ClientAuth(ctx context.Context, req *api.ClientAuthEvent) (*api.Response, error) {
	if req == nil {
		return nil, api.NilRequestError
	}

	status := make(chan bool)
	server.Control <- &ServerEventAuth{
		Id:     req.Id,
		Type:   req.Type,
		Data:   req.Data,
		Status: status,
	}

	res := <-status
	close(status)
	if res {
		return &api.Response{
			Status: api.Status_PERMIT,
		}, nil
	} else {
		return &api.Response{
			Status: api.Status_DENY,
		}, nil
	}
}

func (server *Server) ClientBanner(ctx context.Context, req *api.ClientBannerRequest) (*api.ClientBannerResponse, error) {
	if req == nil {
		return nil, api.NilRequestError
	}

	result := make(chan string)
	server.Control <- &ServerEventBanner{
		Result: result,
	}
	banner := <-result
	close(result)

	return &api.ClientBannerResponse{
		Banner: banner,
	}, nil
}

func (server *Server) IsBanned(ip string, port uint32) bool {
	return false
}

func (server *Server) RenderMessage(client *Client, message *Message) []byte {
	buf := &bytes.Buffer{}
	if message.History {
		buf.WriteString("[" + message.Time.Format("2006-01-02 15:04:05") + "] ")
	} else {
		buf.WriteString("[" + message.Time.Format("15:04:05") + "] ")
	}

	if message.Transient {

	} else {
		if message.Count > 0 {
			num := strconv.FormatUint(message.Count, 10)
			for i := len(num); i < 10; i++ {
				buf.WriteString(" ")
			}
			buf.WriteString(">>" + num + " ")
		}
		if message.SenderIp == "" {
			buf.WriteString("**SERVER** ")
		} else {
			if message.Action {
				buf.WriteString("* " + message.Sender.Name + " ")
			} else {
				buf.WriteString("<" + message.Sender.Name + "> ")
			}
		}
	}
	buf.WriteString(message.Body)
	str := strings.Replace(buf.String()+"\n", "\n", client.Settings.Endlines, -1)
	raw := []byte(str)
	if dat, err := client.Settings.Encoding.NewEncoder().Bytes(raw); err == nil {
		return dat
	} else {
		return raw
	}
}

func (server *Server) Parse(client *Client, message *Message) {
	args := make([]string, 0)
	arg := make([]rune, 0)
	oldstate := 0
	state := ArgSpace
	obody := make([]rune, 0)
	for _, c := range message.Body {
		switch state {
		case ArgText:
			switch c {
			case '\\':
				oldstate = state
				state = ArgEscape
			case ' ':
				obody = append(obody, c)
				state = ArgSpace
				args = append(args, string(arg))
				arg = nil
			default:
				obody = append(obody, c)
				arg = append(arg, c)
			}
		case ArgQuote:
			switch c {
			case '\\':
				oldstate = state
				state = ArgEscape
			case '"':
				obody = append(obody, c)
				state = ArgText
			default:
				obody = append(obody, c)
				arg = append(arg, c)
			}
		case ArgSpace:
			switch c {
			case '\\':
				oldstate = state
				state = ArgEscape
			case '"':
				obody = append(obody, c)
				state = ArgQuote
			case ' ':
				obody = append(obody, c)
			default:
				obody = append(obody, c)
				state = ArgText
				arg = append(arg, c)
			}
		case ArgEscape:
			if (c != '/' && c != '\\') || len(arg) > 0 || len(args) > 0 {
				obody = append(obody, '\\')
			}
			obody = append(obody, c)
			if c != '/' || len(arg) > 0 || len(args) > 0 {
				arg = append(arg, c)
			}
			state = oldstate
		}
	}
	if state != ArgSpace {
		args = append(args, string(arg))
	}
	message.Time = time.Now()
	message.Body = string(obody)

	if len(args) > 0 {
		if handler, ok := Handlers[args[0]]; ok {
			handler.Handler(server, client, message, args)
			return
		}
	}

	server.Broadcast(message)
}

func (server *Server) Process(client *Client, data []byte) bool {
	bodies := make([]string, 0)

	client.Buffer = append(client.Buffer, data...)
	body := make([]byte, 0)
	for _, c := range client.Buffer {
		if c == '\r' || c == '\n' || len(body) >= MessageSizeLimit {
			bodies = append(bodies, string(body))
			body = nil
		}
		body = append(body, c)
	}
	client.Buffer = nil

	for _, bod := range bodies {
		if r, err := chardet.NewTextDetector().DetectBest([]byte(bod)); err == nil {
			if enc, err := ianaindex.IANA.Encoding(r.Charset); err == nil {
				d := enc.NewDecoder()
				if nbody, err := d.String(bod); err == nil {
					bod = nbody
				}
			}
		}
		if len(bod) > 0 {
			message := &Message{
				Sender:   client.Settings,
				SenderId: client.Settings.Id,
				SenderIp: client.Ip,
				Body:     bod,
				Action:   false,
				Count:    0,
			}
			if client.Throttle.Count == 0 || len(client.Queue) > 0 {
				client.Queue = append(client.Queue, message)
			} else {
				server.Parse(client, message)
			}
		}
	}
	return true
}

func (server *Server) Disconnect(client *Client) {
	server.Stream.Send(&api.Command{
		Type: api.CommandType_DISCONNECT_USER,
		Data: &api.Command_DisconnectUser{
			DisconnectUser: &api.DisconnectUserCommand{
				Id: client.Id,
			},
		},
	})
}

func (server *Server) LogLogin(client *Client) {
	if err := server.DB.Insert(&LoginEntity{
		User: client.Settings.Id,
		Ip:   client.Ip,
		Port: client.Port,
		Time: time.Now(),
	}); err != nil {
		logger.Error(err)
	}
}

func (server *Server) Greet(client *Client) {
	history := make([]*MessageEntity, 0)
	if err := server.DB.Model(&history).Limit(10).Order("id desc").Select(); err != nil {
		logger.Error(err)
	}

	buf := &bytes.Buffer{}
	if client.Settings.Id > 0 {
		buf.WriteString("Welcome back to breathe.network!")
		if count, err := server.DB.Model((*MemoEntity)(nil)).Where("receiver = ?", client.Settings.Id).Count(); err == nil && count > 0 {
			n := strconv.FormatUint(uint64(count), 10)
			buf.WriteString("\nYou have " + n + " unread messages! Use /memo to list them.")
		}
	} else {
		buf.WriteString("Welcome to the breathe.network! Start by sending '/endlines win' if you're on windows and read '/help' then.")
	}

	for i := len(history) - 1; i >= 0; i-- {
		h := history[i]
		u := &UserEntity{
			Id: h.SenderId,
		}
		if h.SenderId > 0 {
			if err := server.DB.Select(u); err != nil {
				logger.Error(err)
				u.Id = 0
			}
		}

		server.Send(client, &Message{
			Sender:     UserEntityToSettings(u),
			SenderId:   h.SenderId,
			SenderIp:   h.SenderIp,
			SenderPort: h.SenderPort,
			Body:       h.Body,
			Time:       h.Time,
			Action:     h.Action,
			Count:      h.Id,
			History:    true,
		})
	}
	server.Send(client, &Message{
		Sender:    server.ServerUser.Settings,
		Body:      buf.String(),
		Time:      time.Now(),
		Transient: true,
	})
}

func (server *Server) Send(client *Client, message *Message) {
	server.Stream.Send(&api.Command{
		Type: api.CommandType_SEND_USER,
		Data: &api.Command_SendUser{
			SendUser: &api.SendUserCommand{
				Id:   client.Id,
				Data: server.RenderMessage(client, message),
			},
		},
	})
}

func (server *Server) Broadcast(message *Message) {
	entity := &MessageEntity{
		SenderId:   message.Sender.Id,
		SenderIp:   message.SenderIp,
		SenderPort: message.SenderPort,
		Body:       message.Body,
		Time:       message.Time,
		Action:     message.Action,
	}

	for _, client := range server.Clients {
		if client.Ip == message.SenderIp && client.Port == message.SenderPort && client.Settings.Id == message.SenderId {
			client.Throttle.Count -= 1
			client.Throttle.LastUsed = time.Now()
			break
		}
	}

	if err := server.DB.Insert(entity); err != nil {
		logger.Error(err)
	}
	message.Count = entity.Id
	for _, client := range server.Clients {
		server.Send(client, message)
	}
}

func (server *Server) IsAuthed(client *Client) bool {
	entity := (*AuthEntity)(nil)
	switch client.AuthEntity.Type {
	case api.AuthType_IDENTITY_AUTH_TYPE:
		return true
	case api.AuthType_PASSWORD_AUTH_TYPE:
		ok, err := server.DB.Model(entity).Where("data = ?", client.AuthEntity.Data).Exists()
		return ok && err == nil
	case api.AuthType_PUBKEY_AUTH_TYPE:
		ok, err := server.DB.Model(entity).Where("data = ?", client.AuthEntity.Data).Exists()
		return ok && err == nil
	default:
		return false
	}
}

func UserEntityToSettings(entity *UserEntity) UserSettings {
	if entity.Id == 0 {
		return UserSettings{
			Id:          0,
			Name:        DefaultName,
			Endlines:    DefaultEndlines,
			Encoding:    DefaultEncoding,
			Credit:      DefaultCredit,
			Email:       "",
			EmailPublic: false,
			Signup:      time.Now(),
			Lastseen:    time.Now(),
			Admin:       false,
		}
	}

	enc := DefaultEncoding
	if cand, err := ianaindex.IANA.Encoding(entity.Encoding); err == nil {
		enc = cand
	}

	return UserSettings{
		Id:          entity.Id,
		Name:        entity.Name,
		Endlines:    entity.Endlines,
		Encoding:    enc,
		Credit:      entity.Credit,
		Email:       entity.Email,
		EmailPublic: entity.EmailPublic,
		Signup:      entity.Signup,
		Lastseen:    entity.Lastseen,
		Admin:       entity.Admin,
	}
}

func ClientToUserEntity(client *Client) *UserEntity {
	encname := "utf-8"
	if name, err := ianaindex.IANA.Name(client.Settings.Encoding); err != nil {
		logger.Error(err)
	} else {
		encname = name
	}

	return &UserEntity{
		Id:          client.Settings.Id,
		Name:        client.Settings.Name,
		Endlines:    client.Settings.Endlines,
		Encoding:    encname,
		Email:       client.Settings.Email,
		EmailPublic: client.Settings.EmailPublic,
		Signup:      client.Settings.Signup,
		Lastseen:    client.Settings.Lastseen,
		Credit:      client.Settings.Credit,
		Admin:       client.Settings.Admin,
	}
}

func (server *Server) LoadSettings(client *Client) {
	auth := &AuthEntity{}
	switch client.AuthEntity.Type {
	case api.AuthType_IDENTITY_AUTH_TYPE:
		if err := server.DB.Model(auth).Where("cidr(data) >>= inet(?)", string(client.AuthEntity.Data)).Select(); err == nil {
			client.AuthEntity = *auth
		}
	case api.AuthType_PASSWORD_AUTH_TYPE:
		err := server.DB.Model(auth).Where("data = ?", client.AuthEntity.Data).Select()
		if err == nil {
			client.AuthEntity = *auth
		}
	case api.AuthType_PUBKEY_AUTH_TYPE:
		err := server.DB.Model(auth).Where("data = ?", client.AuthEntity.Data).Select()
		if err == nil {
			client.AuthEntity = *auth
		}
	default:
		return
	}

	user := &UserEntity{}
	if client.AuthEntity.Id > 0 {
		server.DB.Model(user).Where("id = ?", client.AuthEntity.Default).Select()
	}
	client.Settings = UserEntityToSettings(user)

}

func (server *Server) SetAuth(client *Client, auth api.AuthType, data []byte) {
	client.AuthEntity.Type = auth
	switch auth {
	case api.AuthType_IDENTITY_AUTH_TYPE:
		client.AuthEntity.Data = client.Ip
	case api.AuthType_PASSWORD_AUTH_TYPE:
		sha := sha1.New()
		client.AuthEntity.Data = hex.EncodeToString(sha.Sum(data))
	case api.AuthType_PUBKEY_AUTH_TYPE:
		client.AuthEntity.Data = base64.StdEncoding.EncodeToString(data)
	}
}

func (server *Server) ProcessContol(raw ServerEvent) {
	switch event := raw.(type) {
	case *ServerEventCommand:
	case *ServerEventBootstrap:
		logger.Info("boot")
		oldids := make([]string, 0)

		for _, client := range server.Clients {
			oldids = append(oldids, client.Id)
		}

		for _, conn := range event.Clients {
			for i, id := range oldids {
				if id == conn.Id {
					oldids = append(oldids[:i], oldids[i+1:]...)
					break
				}
			}
		}

		for _, old := range oldids {
			if client, ok := server.Clients[old]; ok {
				logger.Info("disc", old)
				delete(server.Clients, old)
				server.Broadcast(&Message{
					Sender:   server.ServerUser.Settings,
					SenderId: server.ServerUser.Settings.Id,
					SenderIp: server.ServerUser.Ip,
					Body:     client.Settings.Name + " disconnected",
					Time:     time.Now(),
					Action:   false,
				})
			}
		}

		for _, conn := range event.Clients {
			client := &Client{
				Id:   conn.Id,
				Ip:   conn.Ip,
				Port: conn.Port,
			}
			server.Clients[conn.Id] = client
			if thr, ok := server.Throttle[client.Ip]; !ok {
				client.Throttle = &Throttle{
					Count:    DefaultCredit,
					LastUsed: time.Now(),
				}
				server.Throttle[client.Ip] = client.Throttle
			} else {
				client.Throttle = thr
			}

			logger.Info("auth", conn.Id, conn.AuthType)
			server.SetAuth(client, conn.AuthType, conn.AuthData)
			if server.IsAuthed(client) {
				server.LoadSettings(client)
			} else {
				server.Disconnect(client)
			}
		}
	case *ServerEventConnected:
		if server.IsBanned(event.Ip, event.Port) {
			event.Status <- false
		} else {
			logger.Info("conn", event.Id, event.Ip, event.Port)
			event.Status <- true
			client := &Client{
				Id:   event.Id,
				Ip:   event.Ip,
				Port: event.Port,
			}
			server.Clients[event.Id] = client
			if thr, ok := server.Throttle[client.Ip]; !ok {
				client.Throttle = &Throttle{
					Count:    DefaultCredit,
					LastUsed: time.Now(),
				}
				server.Throttle[client.Ip] = client.Throttle
			} else {
				client.Throttle = thr
			}
		}
	case *ServerEventDisconnected:
		if client, ok := server.Clients[event.Id]; ok {
			logger.Info("disc", event.Id)
			delete(server.Clients, event.Id)
			server.Broadcast(&Message{
				Sender:   server.ServerUser.Settings,
				SenderId: server.ServerUser.Settings.Id,
				SenderIp: server.ServerUser.Ip,
				Body:     client.Settings.Name + " disconnected",
				Time:     time.Now(),
				Action:   false,
			})
		}
	case *ServerEventAuth:
		if client, ok := server.Clients[event.Id]; ok {
			logger.Info("auth", event.Id, event.Type)
			server.SetAuth(client, event.Type, event.Data)
			if server.IsAuthed(client) {
				server.LoadSettings(client)
				event.Status <- true
				server.LogLogin(client)
				server.Greet(client)
				server.Broadcast(&Message{
					Sender:   server.ServerUser.Settings,
					SenderId: server.ServerUser.Settings.Id,
					SenderIp: server.ServerUser.Ip,
					Body:     client.Settings.Name + " connected",
					Time:     time.Now(),
					Action:   false,
				})
			} else {
				event.Status <- false
			}
		} else {
			event.Status <- false
		}
	case *ServerEventData:
		if client, ok := server.Clients[event.Id]; ok {
			event.Status <- server.Process(client, event.Data)
		} else {
			event.Status <- false
		}
	case *ServerEventBanner:
		event.Result <- ""
	}
}

func (server *Server) Serve() {
	timer := time.NewTimer(1 * time.Second)
	for {
		select {
		case <-timer.C:
			timer.Reset(1 * time.Second)
			for _, client := range server.Clients {
				if client.Settings.Credit == 0 {
					continue
				}
				elapsed := time.Now().Sub(client.Throttle.LastUsed)
				base := time.Minute / time.Duration(client.Settings.Credit)
				add := uint64(elapsed / base)
				if client.Throttle.Count+add > client.Settings.Credit {
					client.Throttle.Count = client.Settings.Credit
				} else {
					client.Throttle.Count += add
				}

				if client.Throttle.Count > 0 && server.Stream != nil {
					proc := 0
					for _, message := range client.Queue {
						server.Parse(client, message)
						proc++
						if client.Throttle.Count == 0 {
							break
						}
					}
					client.Queue = client.Queue[proc:]
				}
			}
		case raw := <-server.Control:
			switch event := raw.(type) {
			case *ServerEventCommand:
				server.Stream = event.Stream
				if server.Stream != nil {
					for _, old := range server.Queue {
						server.ProcessContol(old)
					}
				}
				server.ProcessContol(raw)
			default:
				if server.Stream == nil {
					server.Queue = append(server.Queue, raw)
				} else {
					server.ProcessContol(raw)
				}
			}
		}
	}
}

func NewServer(dbaddr string) *Server {
	if options, err := pg.ParseURL(dbaddr); err != nil {
		panic(err)
	} else {
		options.TLSConfig = nil
		db := pg.Connect(options)
		models := []interface{}{
			(*UserEntity)(nil),
			(*AuthEntity)(nil),
			(*AuthUserEntity)(nil),
			(*LoginEntity)(nil),
			(*MemoEntity)(nil),
			(*MessageEntity)(nil),
			(*BansEntity)(nil),
		}
		for _, model := range models {
			if err := db.CreateTable(model, &orm.CreateTableOptions{
				IfNotExists: true,
			}); err != nil {
				panic(err)
			}
		}
		if res, err := db.Model((*UserEntity)(nil)).Exists(); err != nil {
			logger.Error(err)
		} else if !res {
			srv := &UserEntity{
				Name:        "SERVER",
				Encoding:    "utf-8",
				Endlines:    "\n",
				Email:       "admin@" + DefaultDomain,
				EmailPublic: true,
				Signup:      time.Time{},
				Lastseen:    time.Time{},
				Credit:      DefaultCredit,
				Admin:       true,
			}
			if err := db.Insert(srv); err != nil {
				logger.Error(err)
			} else {
				aut := &AuthEntity{
					Type:    api.AuthType_IDENTITY_AUTH_TYPE,
					Data:    "127.0.0.1/32",
					Default: srv.Id,
				}
				if err := db.Insert(aut); err != nil {
					logger.Error(err)
				} else {
					if err := db.Insert(&AuthUserEntity{
						UserId: srv.Id,
						AuthId: aut.Id,
					}); err != nil {
						logger.Error(err)
					}
				}
			}
		}
		srv := &Server{
			DB:      db,
			Control: make(chan ServerEvent),
			Clients: make(map[string]*Client),
			ServerUser: &Client{
				Id:   "",
				Ip:   "",
				Port: 0,
				AuthEntity: AuthEntity{
					Type: api.AuthType_IDENTITY_AUTH_TYPE,
					Data: "127.0.0.1",
				},
				Throttle: &Throttle{
					Count:    math.MaxUint32,
					LastUsed: time.Now(),
				},
			},
			Throttle: make(map[string]*Throttle),
		}
		srv.LoadSettings(srv.ServerUser)
		return srv
	}
}
