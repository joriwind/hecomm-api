package hecomm

import (
	"encoding/json"
	"fmt"
)

/*
 * hecomm communication definition
 * FPort: 0: DB Command, 10: LinkReq, 50: PK link state, 100: LinkSet, 200: response
 */
const (
	FPortDBCommand int = 0
	FPortLinkReq   int = 10
	FPortLinkState int = 50
	FPortLinkSet   int = 100
	FPortResponse  int = 200
)

/*
 *	Definition of valid EType values
 */
const (
	ETypeNode int = iota + 1
	ETypePlatform
	ETypeLink
)

//Message Structure of top message
type Message struct {
	FPort int
	Data  []byte
}

//DBCommand Structure of Data (DBCommand)
type DBCommand struct {
	Insert bool
	EType  int
	Data   []byte
}

//LinkContract Structure of Data (Link)
type LinkContract struct {
	InfType    int
	ReqDevEUI  []byte
	ProvDevEUI []byte
	Linked     bool
}

//Response Response
type Response struct {
	OK bool
}

const (
	eTypeNode int = iota + 1
	eTypePlatform
	eTypeLink
)

//GetMessage Convert byte slice to HecommMessage
func GetMessage(buf []byte) (*Message, error) {
	var message Message
	err := json.Unmarshal(buf, message)
	if err != nil {
		return &message, err
	}
	return &message, nil
}

//NewMessage Create own Hecomm message
func NewMessage(fPort int, data []byte) ([]byte, error) {
	var message Message
	//Compile message
	message.FPort = fPort
	message.Data = data
	bytes, err := json.Marshal(message)
	return bytes, err
}

//NewResponse Create new response message
func NewResponse(result bool) ([]byte, error) {
	rsp := &Response{OK: result}
	bytes, err := json.Marshal(rsp)
	if err != nil {
		return bytes, err
	}
	message := Message{FPort: FPortResponse, Data: bytes}
	bytes, err = json.Marshal(message)
	return bytes, err
}

//NewDBCommand create new dbcommand message
func NewDBCommand(insert bool, eType int, data []byte) ([]byte, error) {
	dbcommand := DBCommand{
		Insert: insert,
		EType:  eType,
		Data:   data,
	}
	bytes, err := json.Marshal(dbcommand)
	if err != nil {
		return bytes, err
	}
	message := Message{FPort: FPortDBCommand, Data: bytes}
	bytes, err = json.Marshal(message)
	return bytes, err
}

//NewLinkContract Create new LinkContract message
func NewLinkContract(fPort int, reqdev []byte, provdev []byte, inftype int, linked bool) ([]byte, error) {
	lc := LinkContract{
		InfType:    inftype,
		Linked:     linked,
		ProvDevEUI: provdev,
		ReqDevEUI:  reqdev,
	}
	bytes, err := json.Marshal(lc)
	if err != nil {
		return bytes, err
	}
	message := Message{FPort: fPort, Data: bytes}
	bytes, err = json.Marshal(message)
	return bytes, err
}

//GetCommand Convert byte slice of HecommMessage into DBCommand struct
func (m *Message) GetCommand() (*DBCommand, error) {
	var command DBCommand
	if m.FPort != 0 {
		return &command, fmt.Errorf("Hecomm message: FPort not equal to response code: %v", m.FPort)
	}
	err := json.Unmarshal(m.Data, command)
	return &command, err
}

//GetBytes Convert to byte slice
func (m *DBCommand) GetBytes() ([]byte, error) {
	bytes, err := json.Marshal(m)
	return bytes, err

}

//GetLinkContract Convert byte slice of HecommMessage into Link struct
func (m *Message) GetLinkContract() (*LinkContract, error) {
	var link LinkContract
	if m.FPort != FPortLinkReq && m.FPort != FPortLinkSet {
		return &link, fmt.Errorf("Hecomm message: FPort not equal to response code: %v", m.FPort)
	}
	err := json.Unmarshal(m.Data, link)
	return &link, err
}

//GetBytes Convert to byte slice
func (m *LinkContract) GetBytes() ([]byte, error) {
	bytes, err := json.Marshal(m)
	return bytes, err

}

//GetResponse Convert byte slice of HecommMessage into Link struct
func (m *Message) GetResponse() (*Response, error) {
	var rsp Response
	if m.FPort != 200 {
		return &rsp, fmt.Errorf("Hecomm message: FPort not equal to response code: %v", m.FPort)
	}
	err := json.Unmarshal(m.Data, rsp)
	return &rsp, err
}

//GetBytes Convert to byte slice
func (m *Response) GetBytes() ([]byte, error) {
	bytes, err := json.Marshal(m)
	return bytes, err

}

//GetBytes Convert message to byte slice
func (m *Message) GetBytes() ([]byte, error) {
	bytes, err := json.Marshal(m)
	return bytes, err
}
