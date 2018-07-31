package server

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"bytes"
	"github.com/saintfish/chardet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
	"strconv"
	"unicode"
)

const NameAnonymous = "anonymous"

const (
	FlagAction = 1 << iota
	FlagServer
)

const (
	NameLimitMin       = 3
	NameLimitMax       = 32
	MessageLimit       = 1024
	MessagesPerMinute  = 30
	MessageRegenPeriod = time.Minute / MessagesPerMinute
)

const (
	OsWin = "win"
	OsNix = "nix"
)

const (
	EndlineN  = "\n"
	EndlineRN = "\r\n"
)

var ErrNameTaken = errors.New("name taken")
var ErrNameLong = errors.New("name too long")
var ErrNameShort = errors.New("name too short")

type Server interface {
	Broadcast(message *Message)
	Serve(listener net.Listener)
}

type Command func(args []string, message *Message) *Message

func CommandName(args []string, message *Message) *Message {
	if len(args) != 2 {
		message.Sender.ServerMessage("USAGE: /name <nickname>")
		return nil
	}
	oldname := message.Sender.Name
	if message.Sender.Hash > 0 {
		oldname += "#" + fmt.Sprintf("%04d", message.Sender.Hash)
	}
	if err := message.Sender.SetName(args[1], 0); err != nil {
		if err == ErrNameShort {
			message.Sender.ServerMessage("Name '" + args[1] + "' is too short, min " + strconv.FormatUint(uint64(NameLimitMin), 10))
		} else if err == ErrNameLong {
			message.Sender.ServerMessage("Name '" + args[1] + "' is too long, max " + strconv.FormatUint(uint64(NameLimitMax), 10))
		} else {
			message.Sender.ServerMessage("Name '" + args[1] + "' already taken")
		}
	} else {
		message.Sender.Server.BroadcastServer("RENAMED: " + oldname + " -> " + args[1])
	}
	return nil
}

func CommandList(args []string, message *Message) *Message {
	buf := &bytes.Buffer{}
	buf.WriteString("Current users:" + message.Sender.Endline)
	message.Sender.Server.Mutex.Lock()
	for _, c := range message.Sender.Server.Clients {
		buf.WriteString("  * " + c.Name + message.Sender.Endline)
	}
	message.Sender.Server.Mutex.Unlock()
	message.Sender.ServerMessage(buf.String())
	return nil
}

func CommandAction(args []string, message *Message) *Message {
	if len(message.Body) > 4 {
		message.Body = strings.TrimSpace(message.Body[4:])
	} else {
		message.Body = ""
	}
	message.Flags |= FlagAction
	return message
}

func CommandOs(args []string, message *Message) *Message {
	if len(args) != 2 {
		message.Sender.ServerMessage("USAGE: /os <nix | win>")
		return nil
	}
	switch args[1] {
	case OsNix:
		message.Sender.Endline = EndlineN
		if utf8, err := ianaindex.IANA.Encoding("UTF-8"); err != nil {
			fmt.Println(err)
			return nil
		} else {
			message.Sender.Encoding = utf8
		}
		message.Sender.ServerMessage("Encoding set to UTF-8, newlines to \\n")
	case OsWin:
		message.Sender.Endline = EndlineRN
		if win, err := ianaindex.IANA.Encoding("windows-1251"); err != nil {
			fmt.Println(err)
			return nil
		} else {
			message.Sender.Encoding = win
		}
		message.Sender.ServerMessage("Encoding set to windows-1251, newlines to \\r\\n")
	default:
		message.Sender.ServerMessage("USAGE: /os <nix | win>")
	}
	return nil
}

func CommandEncoding(args []string, message *Message) *Message {
	if len(args) != 2 {
		message.Sender.ServerMessage("USAGE: /encoding <IANA name>")
		return nil
	}
	if enc, err := ianaindex.IANA.Encoding(args[1]); err != nil {
		message.Sender.ServerMessage("Invalid IANA encoding name: '" + args[1] + "'")
		return nil
	} else {
		message.Sender.Encoding = enc
		message.Sender.ServerMessage("Encoding set to " + args[1])
	}
	return nil
}

func CommandEndline(args []string, message *Message) *Message {
	if len(args) != 2 {
		message.Sender.ServerMessage("USAGE: /endline <nix | win>")
		return nil
	}
	switch args[1] {
	case OsNix:
		message.Sender.Endline = EndlineN
		message.Sender.ServerMessage("Newlines set to \\n")
	case OsWin:
		message.Sender.Endline = EndlineRN
		message.Sender.ServerMessage("Newlines set to \\r\\n")
	default:
		message.Sender.ServerMessage("USAGE: /endline <nix | win>")
	}
	return nil
}

func CommandHelp(args []string, message *Message) *Message {
	buf := &bytes.Buffer{}
	buf.WriteString("COMMANDS:" + message.Sender.Endline)
	buf.WriteString("  /name     <nickname>  -- sets nickname" + message.Sender.Endline)
	buf.WriteString("  /list                 -- lists connected users" + message.Sender.Endline)
	buf.WriteString("  /me       <text>      -- self-action" + message.Sender.Endline)
	buf.WriteString("  /os       <nix | win> -- sets default encoding and endlines" + message.Sender.Endline)
	buf.WriteString("  /encoding <IANA name> -- sets default encoding" + message.Sender.Endline)
	buf.WriteString("  /endline  <nix | win> -- sets default endlines" + message.Sender.Endline)
	message.Sender.ServerMessage(buf.String())
	return nil
}

var commands = map[string]Command{
	"name":     CommandName,
	"list":     CommandList,
	"me":       CommandAction,
	"os":       CommandOs,
	"encoding": CommandEncoding,
	"endline":  CommandEndline,
	"help":     CommandHelp,
}

type Message struct {
	Sender  *Client
	Body    string
	Flags   uint32
	Counter uint64
	Time    time.Time
}

type Client struct {
	Conn       net.Conn
	Server     *serverImpl
	Connected  time.Time
	Credit     uint32
	Assembly   []byte
	LastSend   time.Time
	Name       string
	Endline    string
	Encoding   encoding.Encoding
	IP         string
	NameCustom bool
	Hash       uint32
}

func (client *Client) ServerMessage(message string) {
	client.Send(&Message{
		Sender:  nil,
		Body:    message,
		Flags:   FlagServer,
		Counter: 0,
		Time:    time.Now(),
	})
}

func (client *Client) SetName(name string, hash uint32) error {
	if strings.ToLower(name) == strings.ToLower(NameAnonymous) {
		client.Name = name
		client.NameCustom = false
		if old, ok := client.Server.ClientHistory[client.IP]; ok {
			old.Name = NameAnonymous
			old.NameCustom = false
		}
		return nil
	}

	l := len(name)
	if l < NameLimitMin {
		return ErrNameShort
	}
	if l > NameLimitMax {
		return ErrNameLong
	}
	for _, n := range client.Server.Clients {
		if strings.ToLower(n.Name) == strings.ToLower(name) && n.Hash == hash {
			return ErrNameTaken
		}
	}
	for _, n := range client.Server.ClientHistory {
		if strings.ToLower(n.Name) == strings.ToLower(name) && n.IP != client.IP {
			return ErrNameTaken
		}
	}
	client.Name = name
	client.Hash = hash
	client.NameCustom = true
	if old, ok := client.Server.ClientHistory[client.IP]; ok {
		old.Name = name
		old.NameCustom = true
	}
	return nil
}

func (client *Client) Serve() {
	defer client.Disconnect()
	buf := make([]byte, 2048)
	client.ServerMessage("Welcome to breathe.network board! Try using `/help` to see what you can do. If you're on windows, try `/endline win` first")

	name := client.Name
	if client.Hash > 0 {
		name += "#" + fmt.Sprintf("%04d", client.Hash)
	}
	client.Server.BroadcastServer("CONNECTED: " + name)
	for {
		if n, err := client.Conn.Read(buf); err != nil {
			break
		} else {
			data := buf[:n]
			for _, b := range data {
				if b == '\r' || b == '\n' {
					if len(client.Assembly) > 0 {
						client.Submit(client.Assembly)
						client.Assembly = client.Assembly[:0]
					}
					continue
				} else {
					client.Assembly = append(client.Assembly, b)
					if len(client.Assembly) >= MessageLimit {
						client.Submit(client.Assembly)
						client.Assembly = client.Assembly[:0]
					}
				}
			}
		}
	}

	name = client.Name
	if client.Hash > 0 {
		name += "#" + fmt.Sprintf("%04d", client.Hash)
	}
	client.Server.BroadcastServer("DISCONNECTED: " + name)
	if old, ok := client.Server.ClientHistory[client.IP]; ok {
		old.Encoding = client.Encoding
		old.Endline = client.Endline
		old.Credit = client.Credit
		old.LastSend = client.LastSend
		old.NameCustom = client.NameCustom
		if client.NameCustom {
			old.Name = client.Name
		}
	}
}

func (client *Client) Submit(body []byte) {
	now := time.Now()
	diff := now.Sub(client.LastSend)
	if diff > MessageRegenPeriod {
		add := uint32(diff / MessageRegenPeriod)
		if client.Credit+add >= MessagesPerMinute {
			client.Credit = MessagesPerMinute
		} else {
			client.Credit += add
		}
	}

	str := strings.TrimSpace(string(body))
	if res, err := client.Server.Detector.DetectBest(body); err == nil {
		if enc, err := ianaindex.IANA.Encoding(res.Charset); err == nil {
			if ndat, _, err := transform.Bytes(enc.NewDecoder(), body); err == nil {
				str = strings.TrimSpace(string(ndat))
			}
		}
	}

	msg := &Message{
		Sender:  client,
		Body:    str,
		Flags:   0,
		Counter: 0,
		Time:    now,
	}

	if client.Credit > 0 {
		client.Server.Queue <- msg
		client.LastSend = now
		client.Credit -= 1
	}
}

func (client *Client) Send(msg *Message) {
	buf := &bytes.Buffer{}

	pref := "[" + msg.Time.Format("15:04:05") + "] "

	name := ""

	if msg.Sender != nil {
		name = msg.Sender.Name
		if msg.Sender.Hash > 0 {
			name += "#" + fmt.Sprintf("%04d", msg.Sender.Hash)
		}
	}

	if msg.Flags&FlagAction != 0 {
		name = "* " + name
	} else {
		name = "<" + name + ">"
	}

	if msg.Flags&FlagServer != 0 {
		name = "SERVER:"
	} else {
		pref += ">>" + strconv.FormatUint(msg.Counter, 10) + " "
	}

	buf.WriteString(pref + name + " " + msg.Body + client.Endline)
	raw := ([]byte)(buf.String())
	if bs, err := client.Encoding.NewEncoder().Bytes(raw); err != nil {
		client.Conn.Write(raw)
	} else {
		client.Conn.Write(bs)
	}
}

func (client *Client) Disconnect() {
	for i, c := range client.Server.Clients {
		if c == client {
			client.Server.Mutex.Lock()
			client.Server.Clients = append(client.Server.Clients[:i], client.Server.Clients[i+1:]...)
			client.Server.Mutex.Unlock()
			break
		}
	}
	client.Conn.Close()
}

type serverImpl struct {
	Clients       []*Client
	Mutex         *sync.Mutex
	Detector      *chardet.Detector
	Queue         chan *Message
	Counter       uint64
	ClientHistory map[string]*Client
}

func (server *serverImpl) PickName() string {
	return NameAnonymous
}

func (server *serverImpl) ProcessQueues() {
	for msg := range server.Queue {
		if len(msg.Body) > 0 && msg.Body[0] == '/' {
			args := make([]string, 0)
			arg := make([]rune, 0)
			escape := false
			quote := false
			skipmatch := false
			for i, c := range msg.Body {
				if i == 0 {
					continue
				} else if i == 1 && c == '/' {
					skipmatch = true
				}
				if unicode.IsSpace(c) && !quote && !escape {
					args = append(args, string(arg))
					arg = arg[:0]
				} else if c == '"' && !escape {
					quote = !quote
				} else if c == '\\' && !escape {
					escape = true
				} else {
					escape = false
					arg = append(arg, c)
				}
			}
			if len(arg) > 0 {
				args = append(args, string(arg))
			}
			low := strings.ToLower(args[0])
			for k, c := range commands {
				if k == low {
					if skipmatch {
						msg.Body = msg.Body[1:]
					} else {
						msg = c(args, msg)
					}
					break
				}
			}
		}
		if msg != nil {
			server.Broadcast(msg)
		}
	}
}

func (server *serverImpl) BroadcastServer(text string) {
	server.Broadcast(&Message{
		Sender:  nil,
		Body:    text,
		Flags:   FlagServer,
		Counter: 0,
		Time:    time.Now(),
	})
}

func (server *serverImpl) Broadcast(message *Message) {
	server.Mutex.Lock()
	server.Counter++
	message.Counter = server.Counter
	for _, client := range server.Clients {
		client.Send(message)
	}
	server.Mutex.Unlock()
}

func (server *serverImpl) Serve(listner net.Listener) {
	go server.ProcessQueues()
	if utf8, err := ianaindex.IANA.Encoding("UTF-8"); err != nil {
		fmt.Println(err)
	} else {
		for {
			if conn, err := listner.Accept(); err != nil {
				fmt.Println(err)
				for _, c := range server.Clients {
					c.Disconnect()
				}
				break
			} else {
				ip := conn.RemoteAddr().(*net.TCPAddr).IP.String()
				client := &Client{
					Conn:       conn,
					Server:     server,
					Connected:  time.Now(),
					Credit:     MessagesPerMinute,
					Assembly:   make([]byte, 0),
					LastSend:   time.Now(),
					Name:       server.PickName(),
					Endline:    EndlineN,
					Encoding:   utf8,
					IP:         ip,
					NameCustom: false,
				}
				if old, ok := server.ClientHistory[ip]; ok {
					client.Encoding = old.Encoding
					client.Endline = old.Endline
					client.Credit = old.Credit
					client.LastSend = old.LastSend
					client.NameCustom = old.NameCustom

					if err := client.SetName(old.Name, 0); err == ErrNameTaken {
						for i := 1; err == ErrNameTaken; i++ {
							err = client.SetName(old.Name, uint32(i))
						}
					}
				} else {
					client.Server.ClientHistory[ip] = client
				}
				server.Mutex.Lock()
				server.Clients = append(server.Clients, client)
				server.Mutex.Unlock()
				go client.Serve()
			}
		}
	}
}

func NewServer() Server {
	return &serverImpl{
		Clients:       make([]*Client, 0),
		Mutex:         &sync.Mutex{},
		Detector:      chardet.NewTextDetector(),
		Queue:         make(chan *Message),
		ClientHistory: make(map[string]*Client),
	}
}
