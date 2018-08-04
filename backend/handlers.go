package backend

import (
	"bytes"
	"encoding/base64"
	"github.com/breathenetwork/chat/api"
	"github.com/breathenetwork/chat/logger"
	"golang.org/x/crypto/ssh"
	"golang.org/x/text/encoding/ianaindex"
	"sort"
	"strconv"
	"strings"
	"time"
)

type HandlerDesc struct {
	Handler     Handler
	Args        string
	Description string
}

var Handlers map[string]*HandlerDesc

func init() {
	Handlers = map[string]*HandlerDesc{
		"/me":            {Handler: ActionHandler, Args: "", Description: "self-action"},
		"/quit":          {Handler: QuitHandler, Args: "", Description: "quits board"},
		"/name":          {Handler: NameHandler, Args: "<name>", Description: "sets your name"},
		"/register":      {Handler: RegisterHandler, Args: "<name> <ssh key b64> [email] [email public]", Description: "registers user"},
		"/login":         {Handler: LoginHandler, Args: "<name>", Description: "logins as user"},
		"/logout":        {Handler: LogoutHandler, Args: "", Description: "logouts as user"},
		"/help":          {Handler: HelpHandler, Args: "", Description: "prints help"},
		"/whois":         {Handler: WhoisHandler, Args: "<name>", Description: "prints whois on user"},
		"/email":         {Handler: EmailHandler, Args: "<email> [email public]", Description: "sets new email"},
		"/encoding":      {Handler: EncodingHandler, Args: "<encoding>", Description: "sets encoding"},
		"/endlines":      {Handler: EndlinesHandler, Args: "<win|nix>", Description: "sets newlines"},
		"/history":       {Handler: HistoryHandler, Args: "<limit> [offset]", Description: "history log"},
		"/historyday":    {Handler: HistorydayHandler, Args: "<2006-12-31> [limit] [offset]", Description: "history log for day"},
		"/historysearch": {Handler: HistorysearchHandler, Args: "<t*rm> [limit] [offset]", Description: "history log for day"},
		"/memorise":      {Handler: MemoriseHandler, Args: "<user> <text>", Description: "send a memo to user"},
		"/memo":          {Handler: MemoHandler, Args: "[id] [delete]", Description: "read a memo"},
	}
}

func FormatUsage(name string) string {
	entry := Handlers[name]
	return "USAGE: " + name + " " + entry.Args + " -- " + entry.Description
}

func ActionHandler(server *Server, client *Client, message *Message, args []string) {
	message.Action = true
	message.Body = strings.TrimSpace(message.Body[3:])
	server.Broadcast(message)
}

func QuitHandler(server *Server, client *Client, message *Message, args []string) {
	server.Disconnect(client)
}

func NameHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if strings.ToLower(client.Settings.Name) == strings.ToLower(DefaultName) {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "register & authenticate first",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	newname := strings.TrimSpace(args[1])

	if len(newname) < NameMinSize {
		n := strconv.FormatUint(uint64(NameMinSize), 10)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "name is too short, must not be shorter than " + n,
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if len(newname) > NameMaxSize {
		n := strconv.FormatUint(uint64(NameMaxSize), 10)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "name is too long, must not be longer than " + n,
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if res, err := server.DB.Model((*UserEntity)(nil)).Where("lower(name) = lower(?)", newname).Exists(); err != nil {
		logger.Error(err)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "database error",
			Time:      time.Now(),
			Transient: true,
		})
	} else {
		if strings.ToLower(client.Settings.Name) != strings.ToLower(newname) && res {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "name is already taken",
				Time:      time.Now(),
				Transient: true,
			})
		} else {
			oldname := client.Settings.Name
			client.Settings.Name = newname
			entity := ClientToUserEntity(client)
			if err := server.DB.Update(entity); err != nil {
				logger.Error(err)
			}
			server.Broadcast(&Message{
				Sender:   server.ServerUser.Settings,
				SenderId: server.ServerUser.Settings.Id,
				SenderIp: server.ServerUser.Ip,
				Body:     oldname + " renamed to " + client.Settings.Name,
				Time:     time.Now(),
				Action:   false,
			})
		}
	}
}

func RegisterHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 3 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if strings.ToLower(client.Settings.Name) != strings.ToLower(DefaultName) {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "logout first",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	newname := strings.TrimSpace(args[1])

	if len(newname) < NameMinSize {
		n := strconv.FormatUint(uint64(NameMinSize), 10)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "name is too short, must not be shorter than " + n,
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if len(newname) > NameMaxSize {
		n := strconv.FormatUint(uint64(NameMaxSize), 10)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "name is too long, must not be longer than " + n,
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if res, err := server.DB.Model((*UserEntity)(nil)).Where("lower(name) = lower(?)", newname).Exists(); err != nil {
		logger.Error(err)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "database error",
			Time:      time.Now(),
			Transient: true,
		})
	} else {
		if res {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "name is already taken",
				Time:      time.Now(),
				Transient: true,
			})
		} else {
			if data, err := base64.StdEncoding.DecodeString(args[2]); err != nil {
				server.Send(client, &Message{
					Sender:    server.ServerUser.Settings,
					Body:      "invalid base64",
					Time:      time.Now(),
					Transient: true,
				})
			} else {
				if pub, err := ssh.ParsePublicKey(data); err != nil {
					server.Send(client, &Message{
						Sender:    server.ServerUser.Settings,
						Body:      "invalid ssh public key",
						Time:      time.Now(),
						Transient: true,
					})
				} else {
					entity := ClientToUserEntity(client)
					entity.Name = newname
					if len(args) > 3 {
						entity.Email = strings.TrimSpace(args[3])
					}
					entity.EmailPublic = len(args) < 4 || strings.ToLower(strings.ToLower(args[4])) == "true"
					if err := server.DB.Insert(entity); err != nil {
						logger.Error(err)
						server.Send(client, &Message{
							Sender:    server.ServerUser.Settings,
							Body:      "database error",
							Time:      time.Now(),
							Transient: true,
						})
						return
					}

					b64 := base64.StdEncoding.EncodeToString(pub.Marshal())
					auth := &AuthEntity{
						Type:    api.AuthType_PUBKEY_AUTH_TYPE,
						Data:    b64,
						Default: entity.Id,
					}

					if err := server.DB.Insert(auth); err != nil {
						logger.Error(err)
						server.Send(client, &Message{
							Sender:    server.ServerUser.Settings,
							Body:      "database error",
							Time:      time.Now(),
							Transient: true,
						})
						return
					}

					if err := server.DB.Insert(&AuthUserEntity{
						UserId: entity.Id,
						AuthId: auth.Id,
					}); err != nil {
						logger.Error(err)
						server.Send(client, &Message{
							Sender:    server.ServerUser.Settings,
							Body:      "database error",
							Time:      time.Now(),
							Transient: true,
						})
						return
					}

					server.Send(client, &Message{
						Sender:    server.ServerUser.Settings,
						Body:      "Successfully registered! Now you can login via ssh -T " + DefaultDomain + " -p 31337",
						Time:      time.Now(),
						Transient: true,
					})
				}
			}
		}
	}
}

func LoginHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if strings.ToLower(client.Settings.Name) != strings.ToLower(DefaultName) {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "logout first",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	newname := strings.TrimSpace(args[1])

	user := &UserEntity{}
	if err := server.DB.Model(user).Where("lower(name) = lower(?)", newname).Select(); err != nil {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "user do not exist",
			Time:      time.Now(),
			Transient: true,
		})
	} else {
		mapping := make([]*AuthUserEntity, 0)
		if res, err := server.DB.Model(&mapping).Where("auth_id = ? and user_id = ?", client.AuthEntity.Id, user.Id).Exists(); err != nil {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "database error",
				Time:      time.Now(),
				Transient: true,
			})
		} else if !res {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "not authorized",
				Time:      time.Now(),
				Transient: true,
			})
		} else {
			oldname := client.Settings.Name
			client.Settings = UserEntityToSettings(user)
			server.Broadcast(&Message{
				Sender:   server.ServerUser.Settings,
				SenderId: server.ServerUser.Settings.Id,
				SenderIp: server.ServerUser.Ip,
				Body:     oldname + " renamed to " + client.Settings.Name,
				Time:     time.Now(),
				Action:   false,
			})
		}
	}
}

func LogoutHandler(server *Server, client *Client, message *Message, args []string) {
	if strings.ToLower(client.Settings.Name) == strings.ToLower(DefaultName) {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "register & authenticate first",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	oldname := client.Settings.Name
	client.Settings = UserEntityToSettings(&UserEntity{})
	server.Broadcast(&Message{
		Sender:   server.ServerUser.Settings,
		SenderId: server.ServerUser.Settings.Id,
		SenderIp: server.ServerUser.Ip,
		Body:     oldname + " renamed to " + client.Settings.Name,
		Time:     time.Now(),
		Action:   false,
	})
}

func HelpHandler(server *Server, client *Client, message *Message, args []string) {
	longkey := 0
	longargs := 0
	keys := make([]string, 0)
	for key, h := range Handlers {
		if len(key) > longkey {
			longkey = len(key)
		}

		if len(h.Args) > longargs {
			longargs = len(h.Args)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buf := &bytes.Buffer{}
	for i, key := range keys {
		if i > 0 {
			buf.WriteString("\n")
		}
		h := Handlers[key]
		buf.WriteString("    ")
		buf.WriteString(key)
		for i := len(key); i < longkey; i++ {
			buf.WriteString(" ")
		}
		buf.WriteString(" ")
		buf.WriteString(h.Args)
		for i := len(h.Args); i < longargs; i++ {
			buf.WriteString(" ")
		}
		buf.WriteString("  -- ")
		buf.WriteString(h.Description)
	}
	server.Send(client, &Message{
		Sender:    server.ServerUser.Settings,
		Body:      "available commands:\n" + buf.String(),
		Time:      time.Now(),
		Transient: true,
	})
}

func WhoisHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	newname := strings.TrimSpace(args[1])

	if strings.ToLower(newname) == strings.ToLower(DefaultName) {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "user is anonymous",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	user := &UserEntity{}
	if err := server.DB.Model(user).Where("lower(name) = lower(?)", newname).Select(); err != nil {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "user do not exist",
			Time:      time.Now(),
			Transient: true,
		})
	} else {
		email := user.Email
		if !user.EmailPublic {
			email = "<hidden>"
		}
		endlines := "nix"
		if user.Endlines == "\r\n" {
			endlines = "win"
		}
		credit := strconv.FormatUint(user.Credit, 10)
		buf := &bytes.Buffer{}
		buf.WriteString("user info on " + user.Name + ":\n")
		buf.WriteString("    Signed up: " + user.Signup.Format("2006-01-02 15:04:05") + "\n")
		buf.WriteString("    Last seen: " + user.Lastseen.Format("2006-01-02 15:04:05") + "\n")
		buf.WriteString("        Email: " + email + "\n")
		buf.WriteString("     Encoding: " + user.Encoding + "\n")
		buf.WriteString("     Endlines: " + endlines + "\n")
		buf.WriteString("  Post credit: " + credit + "")
		if user.Admin {
			buf.WriteString("\n               User is administrator")
		}

		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      buf.String(),
			Time:      time.Now(),
			Transient: true,
		})
	}
}

func EmailHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if strings.ToLower(client.Settings.Name) == strings.ToLower(DefaultName) {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "register & authenticate first",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	client.Settings.Email = strings.TrimSpace(args[1])
	if len(args) > 2 {
		client.Settings.EmailPublic = strings.TrimSpace(strings.ToLower(args[2])) == "true"
	}
	if err := server.DB.Update(ClientToUserEntity(client)); err != nil {
		logger.Error(err)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "database error",
			Time:      time.Now(),
			Transient: true,
		})
	} else {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "email updated",
			Time:      time.Now(),
			Transient: true,
		})
	}
}

func EncodingHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	enc := strings.TrimSpace(strings.ToLower(args[1]))

	if encoding, err := ianaindex.IANA.Encoding(enc); err != nil {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "unknown encoding name",
			Time:      time.Now(),
			Transient: true,
		})
	} else {
		client.Settings.Encoding = encoding
		if client.Settings.Id > 0 {
			if err := server.DB.Update(ClientToUserEntity(client)); err != nil {
				logger.Error(err)
				server.Send(client, &Message{
					Sender:    server.ServerUser.Settings,
					Body:      "database error",
					Time:      time.Now(),
					Transient: true,
				})
			}
		}
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "encoding updated",
			Time:      time.Now(),
			Transient: true,
		})
	}
}

func EndlinesHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	endl := strings.TrimSpace(strings.ToLower(args[1]))
	switch endl {
	case "nix":
		client.Settings.Endlines = "\n"
	case "win":
		client.Settings.Endlines = "\r\n"
	default:
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "unknown endline type",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if client.Settings.Id > 0 {
		if err := server.DB.Update(ClientToUserEntity(client)); err != nil {
			logger.Error(err)
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "database error",
				Time:      time.Now(),
				Transient: true,
			})
		}
	}
	server.Send(client, &Message{
		Sender:    server.ServerUser.Settings,
		Body:      "endlines updated",
		Time:      time.Now(),
		Transient: true,
	})
}

func HistoryHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	offset := 0
	limit := 10

	if lim, err := strconv.ParseUint(strings.TrimSpace(args[1]), 10, 32); err != nil {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "invalid limit",
			Time:      time.Now(),
			Transient: true,
		})
		return
	} else {
		limit = int(lim)
	}

	if len(args) > 2 {
		if off, err := strconv.ParseUint(strings.TrimSpace(args[2]), 10, 32); err != nil {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "invalid offset",
				Time:      time.Now(),
				Transient: true,
			})
			return
		} else {
			offset = int(off)
		}
	}

	if limit > 100 {
		limit = 100
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "lowering limit to 100",
			Time:      time.Now(),
			Transient: true,
		})
	}

	history := make([]*MessageEntity, 0)
	if err := server.DB.Model(&history).Limit(limit).Offset(offset).Order("id desc").Select(); err != nil {
		logger.Error(err)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "database error",
			Time:      time.Now(),
			Transient: true,
		})
		return
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
}

func HistorydayHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	limit := 10
	offset := 0

	if len(args) > 2 {
		if lim, err := strconv.ParseUint(strings.TrimSpace(args[2]), 10, 32); err != nil {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "invalid limit",
				Time:      time.Now(),
				Transient: true,
			})
			return
		} else {
			limit = int(lim)
		}
	}

	if len(args) > 3 {
		if off, err := strconv.ParseUint(strings.TrimSpace(args[3]), 10, 32); err != nil {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "invalid offset",
				Time:      time.Now(),
				Transient: true,
			})
			return
		} else {
			offset = int(off)
		}
	}

	if limit > 100 {
		limit = 100
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "lowering limit to 100",
			Time:      time.Now(),
			Transient: true,
		})
	}

	if t, err := time.Parse("2006-01-02", strings.TrimSpace(args[1])); err != nil {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "invalid day format",
			Time:      time.Now(),
			Transient: true,
		})
	} else {
		history := make([]*MessageEntity, 0)
		if err := server.DB.Model(&history).Where("date(time) = date(?)", t).Limit(limit).Offset(offset).Order("id desc").Select(); err != nil {
			logger.Error(err)
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "database error",
				Time:      time.Now(),
				Transient: true,
			})
			return
		}

		for i := len(history) - 1; i >= 0; i-- {
			h := history[i]
			u := &UserEntity{
				Id: h.SenderId,
			}
			if h.SenderId > 0 {
				if err := server.DB.Select(u); err != nil {
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
	}
}

func HistorysearchHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 2 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	term := strings.Replace(strings.Replace(strings.TrimSpace(args[1]), "%", "\\%", -1), "*", "%", -1)

	limit := 10
	offset := 0

	if len(args) > 2 {
		if lim, err := strconv.ParseUint(strings.TrimSpace(args[2]), 10, 32); err != nil {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "invalid limit",
				Time:      time.Now(),
				Transient: true,
			})
			return
		} else {
			limit = int(lim)
		}
	}

	if len(args) > 3 {
		if off, err := strconv.ParseUint(strings.TrimSpace(args[3]), 10, 32); err != nil {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "invalid offset",
				Time:      time.Now(),
				Transient: true,
			})
			return
		} else {
			offset = int(off)
		}
	}

	if limit > 100 {
		limit = 100
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "lowering limit to 100",
			Time:      time.Now(),
			Transient: true,
		})
	}

	history := make([]*MessageEntity, 0)
	if err := server.DB.Model(&history).Where("lower(body) like lower(?)", term).Limit(limit).Offset(offset).Order("id desc").Select(); err != nil {
		logger.Error(err)
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "database error",
			Time:      time.Now(),
			Transient: true,
		})
		return
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
}

func MemoriseHandler(server *Server, client *Client, message *Message, args []string) {
	if len(args) < 3 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	user := strings.TrimSpace(args[1])
	body := make([]rune, 0)
	for i := 2; i < len(args); i++ {
		if i > 2 {
			body = append(body, ' ')
		}
		body = append(body, ([]rune)(args[i])...)
	}
	if len(body) == 0 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      FormatUsage(args[0]),
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	entity := &UserEntity{}
	if err := server.DB.Model(entity).Where("lower(name) = lower(?)", user).Select(); err != nil {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "unknown user",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}
	memo := &MemoEntity{
		Sender:   client.Settings.Id,
		Receiver: entity.Id,
		Body:     string(body),
		Time:     time.Now(),
	}
	if err := server.DB.Insert(memo); err != nil {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "database error",
			Time:      time.Now(),
			Transient: true,
		})
	} else {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "memo saved",
			Time:      time.Now(),
			Transient: true,
		})
	}
}

func MemoHandler(server *Server, client *Client, message *Message, args []string) {
	if client.Settings.Id == 0 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "register & authenticate first",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	memos := make([]*MemoEntity, 0)
	if err := server.DB.Model(&memos).Where("receiver = ?", client.Settings.Id).Order("id asc").Select(); err != nil || len(memos) == 0 {
		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "no memos",
			Time:      time.Now(),
			Transient: true,
		})
		return
	}

	if len(args) > 1 {
		if id, err := strconv.ParseUint(strings.TrimSpace(args[1]), 10, 32); err != nil || int(id) > len(memos)-1 {
			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      "invalid memo id",
				Time:      time.Now(),
				Transient: true,
			})
		} else {
			memo := memos[id]
			buf := &bytes.Buffer{}
			buf.WriteString("memo from ")
			user := &UserEntity{
				Id: memo.Sender,
			}
			if err := server.DB.Select(user); err != nil {
				server.Send(client, &Message{
					Sender:    server.ServerUser.Settings,
					Body:      "database error",
					Time:      time.Now(),
					Transient: true,
				})
			} else {
				buf.WriteString(user.Name + " sent @ " + memo.Time.Format("2006-01-02 15:04:05") + ":\n")
			}
			buf.WriteString("    ")
			buf.WriteString(memo.Body)

			if len(args) > 2 {
				if strings.ToLower(strings.TrimSpace(args[2])) == "true" {
					server.DB.Delete(memo)
				}
				buf.WriteString("\n\nmemo deleted.")
			}

			server.Send(client, &Message{
				Sender:    server.ServerUser.Settings,
				Body:      buf.String(),
				Time:      time.Now(),
				Transient: true,
			})
		}
	} else {
		buf := &bytes.Buffer{}
		for i, memo := range memos {
			if i > 0 {
				buf.WriteString("\n")
			}
			n := strconv.FormatUint(uint64(i), 10)
			buf.WriteString("    [" + n + "]    ")
			buf.WriteString(memo.Time.Format("2006-01-02 15:04:05") + "    by ")
			user := &UserEntity{
				Id: memo.Sender,
			}
			if err := server.DB.Select(user); err != nil {
				server.Send(client, &Message{
					Sender:    server.ServerUser.Settings,
					Body:      "database error",
					Time:      time.Now(),
					Transient: true,
				})
			} else {
				buf.WriteString(user.Name)
			}

			buf.WriteString("    ")
			prev := memo.Body
			if len(prev) > 32 {
				prev = prev[:32] + "..."
			}

			buf.WriteString(prev)
		}

		server.Send(client, &Message{
			Sender:    server.ServerUser.Settings,
			Body:      "your memos:\n" + buf.String(),
			Time:      time.Now(),
			Transient: true,
		})
	}
}
