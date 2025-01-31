package palrcon

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gorcon/rcon"
)

type Player struct {
	Name      string
	PlayerUID string // might be int64
	SteamID   string // might be int64
}

type PalRCON interface {
	GetPlayers() ([]Player, error)
	Broadcast(message string) error
	SetTimeout(timeout time.Duration)
}

func NewPalRCON(endpoint, password string) PalRCON {
	return &palRCON{
		endpoint: endpoint,
		password: password,
	}
}

type palRCON struct {
	endpoint string
	password string

	timeout time.Duration
}

func (p *palRCON) execute(command string) (string, error) {
	// rcon of palworld in unstable
	// so the connection isn't reused

	rconn, err := rcon.Dial(
		p.endpoint, p.password,
		rcon.SetDialTimeout(p.timeout),
		rcon.SetDeadline(p.timeout),
	)

	if err != nil {
		return "", fmt.Errorf("failed to connect to %s: %w", p.endpoint, err)
	}
	defer rconn.Close()

	result, err := rconn.Execute(command)

	if err != nil {
		return result, fmt.Errorf("failed to execute the command: %w", err)
	}

	if len(result) == 0 {
		return result, nil
	}

	raw := []byte(result)
	i := len(raw)
	for ; i > 0; i-- {
		if raw[i-1] != 0 {
			break
		}
	}

	return string(raw[:i]), nil
}

func (p *palRCON) GetPlayers() ([]Player, error) {
	// ShowPlayers often times out, so ignore the error

	result, err := p.execute("ShowPlayers")

	if len(result) == 0 && err != nil {
		return nil, err
	}

	lines := strings.Split(result, "\n")[1:] // skip header (name,playeruid,steamid)

	players := make([]Player, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		fields := strings.Split(line, ",")
		name := []byte(strings.Join(fields[:1], ","))
		if len(fields) == 0 {
			continue
		}

		if len(fields) == 1 {
			players = append(players, Player{
				Name:      extractPrintableChars(name),
				PlayerUID: "",
				SteamID:   "",
			})
		} else if len(fields) == 2 {
			players = append(players, Player{
				Name:      extractPrintableChars(name),
				PlayerUID: extractPrintableChars([]byte(fields[1])),
				SteamID:   "",
			})
		} else {
			players = append(players, Player{
				Name:      extractPrintableChars(name),
				PlayerUID: extractPrintableChars([]byte(fields[1])),
				SteamID:   extractPrintableChars([]byte(fields[2])),
			})
		}
	}

	return players, nil
}

func extractPrintableChars(input []byte) string {
	var result []rune

	for len(input) > 0 {
		r, size := utf8.DecodeRune(input)
		if unicode.IsPrint(r) {
			result = append(result, r)
		}
		input = input[size:]
	}

	return string(result)
}

func (p *palRCON) Broadcast(message string) error {
	_, err := p.execute(fmt.Sprintf("Broadcast %s", message))

	return err
}

func (p *palRCON) SetTimeout(timeout time.Duration) {
	p.timeout = timeout
}
