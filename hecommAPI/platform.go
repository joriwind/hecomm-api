package hecommAPI

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"time"

	"fmt"

	"github.com/joriwind/hecomm-api/hecomm"
)

const (
	//KeySize Size of the key
	KeySize int = 32
)

//Platform Struct defining the hecomm server
type Platform struct {
	ctx     context.Context
	Address string
	Config  *tls.Config
	Nodes   map[string]*nodeType
	pushKey func(deveui []byte, key []byte) error
}

//nodeType Used to define the nodes linked to the hecomm system
type nodeType struct {
	DevEUI []byte
	Link   linkType
}

//linkType A link between node and the contract
type linkType struct {
	contract hecomm.LinkContract
	osSKey   [KeySize]byte
}

//NewPlatform Create new hecomm server API
func NewPlatform(ctx context.Context, address string, config *tls.Config, nodes [][]byte, callback func(deveui []byte, key []byte) error) (*Platform, error) {
	var pl Platform

	pl.ctx = ctx
	pl.Address = address
	//config.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA}
	pl.Config = config
	pl.Nodes = make(map[string]*nodeType)
	for i, val := range nodes {
		pl.Nodes[string(nodes[i])] = &nodeType{DevEUI: val}
	}
	pl.pushKey = callback

	return &pl, nil
}

//AddNode add another node for hecomm communication
func (pl *Platform) AddNode(node []byte) {
	log.Printf("Inserted node into hecommAPI: %v\n", string(node))
	pl.Nodes[string(node)] = &nodeType{DevEUI: node}
}

//Start Start listening
func (pl *Platform) Start() error {
	listener, err := tls.Listen("tcp", pl.Address, pl.Config)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("Hecomm platform listening on: %v\n", pl.Address)
	chanConn := make(chan net.Conn, 1)
	//Listen on tls port
	for {
		//Wait for connection!
		go func(listener net.Listener, chanConn chan net.Conn) {
			conn, err := listener.Accept()

			if err != nil {
				log.Printf("hecommplatform server: did not accept: %v\n", err)
				return
			}
			chanConn <- conn
			return
		}(listener, chanConn)

		//Check if connection available or context
		select {
		case conn := <-chanConn:
			remote, _, err := net.SplitHostPort(conn.RemoteAddr().String())
			if err != nil {
				log.Fatalf("Could not split address: %v, %v\n", conn.RemoteAddr().String(), err)
			}
			fog, _, err := net.SplitHostPort(fogAddress)
			if err != nil {
				log.Fatalf("Could not split address: %v, %v\n", fogAddress, err)
			}
			if remote == fog {
				pl.handleProviderConnection(conn)
			} else {
				log.Printf("hecommplatform server: wrong connection: %v != %v\n", remote, fog)
			}
		case <-pl.ctx.Done():
			return nil
		}

	}

}

func (pl *Platform) handleProviderConnection(conn net.Conn) {
	buf := make([]byte, 4048)

	//The to be created link
	var link linkType
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Printf("hecommplatform server: handleConnection: could not read: %v\n", err)
			break
		}

		message, err := hecomm.GetMessage(buf[:n])
		if err != nil {
			log.Fatalln("Unable to decipher bytes on hecomm channel!")
		}

		log.Printf("Received message, fPort: %v", message.FPort)
		log.Printf("Message: %+v\n", message)

		switch message.FPort {
		case hecomm.FPortLinkReq:
			//Link request from fog
			lc, err := message.GetLinkContract()
			if err != nil {
				log.Printf("hecommplatform server: handleConnection: unvalid LinkContract: error: %v\n", err)
			}
			if lc.Linked {
				log.Printf("Unexpected LinkContract in LinkReq, is already linked: %v\n", lc)
			}

			//Find the requested node
			node, ok := pl.Nodes[string(lc.ProvDevEUI[:])]
			//Check if valid node is found --> node.DevEUI not nil or something
			if !ok {
				log.Printf("hecommplatform server: handleconnection: could not find node: %v\n", string(lc.ProvDevEUI[:]))
				rsp, err := hecomm.NewResponse(false)
				if err != nil {
					log.Fatalf("Failed to create response: error: %v\n", err)
					return
				}
				_, err = conn.Write(rsp)
				if err != nil {
					log.Fatalf("hecommplatform server: handleConnection: failed to send response: %v\n", err)
					return
				}
				return
			}

			//Check if node already has connection!
			if ok := node.Link.contract.Linked; ok {
				log.Printf("Unable to connect to this node, already connected: %v\n", lc)
				rsp, err := hecomm.NewResponse(false)
				if err != nil {
					log.Fatalf("Failed to create response: error: %v\n", err)
					return
				}
				_, err = conn.Write(rsp)
				if err != nil {
					log.Fatalf("hecommplatform server: handleConnection: failed to send response: %v\n", err)
					return
				}
				break

			}

			//Send positive response and start PK state
			rsp, err := hecomm.NewResponse(true)
			if err != nil {
				log.Fatalf("hecommplatform server: handleConnection: failed to create response: %v\n", err)
				return
			}
			_, err = conn.Write(rsp)
			if err != nil {
				log.Fatalf("hecommplatform server: handleConnection: failed to send response: %v\n", err)
				return
			}

			//Add to temp link
			//Check if already started linking
			if link.contract.ProvDevEUI != nil {
				log.Printf("Already start linking!?: Link: %v LinkContract: %v\n", link, lc)
				break
			}
			link.contract = *lc

			err = pl.bob(&link, conn, *message)
			if err != nil {
				log.Printf("DH protocol error: %v\n", err)
				break
			}

		case hecomm.FPortLinkState:
			if link.contract.ProvDevEUI == nil {
				log.Printf("The link protocol has not started yet?: %v\n", link)
			}

		case hecomm.FPortLinkSet:
			//Link set from fog
			lc, err := message.GetLinkContract()
			if err != nil {
				log.Printf("hecommplatform server: handleConnection: unvalid LinkContract: error: %v\n", err)
			}

			//Check corresponds with active linking
			for index, b := range link.contract.ProvDevEUI {
				if b != lc.ProvDevEUI[index] {
					log.Fatalf("LinkSet does not correspond with active link!: Link: %v, LinkSet: %v\n", link, lc)
					return
				}
			}

			//Check if shared key is set
			if link.osSKey == [KeySize]byte{} {
				log.Printf("Active link does not have a shared key: %v", link)
				break
			}

			//Push key to node
			//Get corresponding node
			node, ok := pl.Nodes[string(lc.ProvDevEUI[:])]
			if !ok {
				log.Printf("The node isn't available anymore?\n")

				//Send bad response
				bytes, err := hecomm.NewResponse(false)
				if err != nil {
					log.Fatalf("Could not create hecomm false response: %v\n", err)
				}
				conn.Write(bytes)
				return
			}
			//Add item, pushing key down to node
			err = pl.pushKey(node.DevEUI, link.osSKey[:])
			if err != nil {
				fmt.Printf("hecommplatform server: failed to push osSKey: %v\n", err)

				//Send bad response
				bytes, err := hecomm.NewResponse(false)
				if err != nil {
					log.Fatalf("Could not create hecomm false response: %v\n", err)
				}
				conn.Write(bytes)
				return
			}
			//Define link in state
			pl.Nodes[string(node.DevEUI)].Link = link
			log.Printf("Link is set: %v\n", link)
			//Clear link
			link = linkType{}

			//Send ok response
			bytes, err := hecomm.NewResponse(true)
			if err != nil {
				log.Fatalf("Could not create hecomm true response: %v\n", err)
			}
			conn.Write(bytes)

		default:
			log.Printf("Unexpected FPort in hecomm message: %v\n", message)
		}

	}
}

//RequestLink Requester side of hecomm protocol
func (pl *Platform) RequestLink(deveui []byte, infType int) error {
	conn, err := tls.Dial("tcp", fogAddress, pl.Config)
	if err != nil {
		return err
	}
	defer conn.Close()

	buf := make([]byte, 4048)

	//The to be created link
	link := linkType{
		contract: hecomm.LinkContract{
			InfType:   infType,
			ReqDevEUI: deveui,
		},
	}
	bytes, err := link.contract.GetBytes()
	if err != nil {
		return err
	}
	request, err := hecomm.NewMessage(hecomm.FPortLinkReq, bytes)
	if err != nil {
		return err
	}

	//Sending request
	conn.Write(request)

	for {
		//Wait for response
		n, err := conn.Read(buf)
		fmt.Printf("Connection state: %+v\n", conn.ConnectionState())
		if err != nil {
			return err
		}
		//Decode response
		message, err := hecomm.GetMessage(buf[:n])
		if err != nil {
			return err
		}
		log.Printf("Received message, fPort: %v\n", message.FPort)
		log.Printf("Message: %+v\n", message)
		switch message.FPort {
		case hecomm.FPortLinkReq:
			//Expecting response with provider identification
			lc, err := message.GetLinkContract()
			if err != nil {
				return err
			}
			//Add providder identification to linkcontract
			if lc.ProvDevEUI == nil {
				return fmt.Errorf("Expected an non nil provider deveui")
			}
			link.contract.ProvDevEUI = lc.ProvDevEUI

			//Startup PK
			err = pl.alice(&link, conn, buf)
			if err != nil {
				return err
			}
			//Key has been established --> thus linked
			link.contract.Linked = true

			//Send set
			//Encode lc
			bytes, err = link.contract.GetBytes()
			if err != nil {
				return err
			}
			//Create hecomm message
			setReq, err := hecomm.NewMessage(hecomm.FPortLinkSet, bytes)
			if err != nil {
				return err
			}
			//Send the hecomm message
			conn.Write(setReq)

		case hecomm.FPortResponse:
			resp, err := message.GetResponse()
			if err != nil {
				return err
			}
			if resp.OK {
				//Set link to node
				pl.Nodes[string(link.contract.ReqDevEUI[:])].Link = link
				//Push the key down to the fog
				err := pl.pushKey(link.contract.ReqDevEUI, link.osSKey[:])
				if err != nil {
					return err
				}
				//Link is set, return now
				return nil

			} else {
				if link.contract.Linked == true {
					return fmt.Errorf("Hecomm protocol Set link failed")
				} else {
					return fmt.Errorf("Hecomm protocol Request link failed")
				}
			}
		default:
			return fmt.Errorf("Unkown or unsupported FPORT: %v", message.FPort)
		}

	}
}

//alice Initiator side from DH perspective
func (pl Platform) alice(link *linkType, conn net.Conn, buf []byte) error {
	fmt.Printf("Alice started\n")
	//Get default group

	/* var keywriter = keyWriter{}
	tlsc := tls.Config{
		KeyLogWriter: keywriter,
	}
	tls.Client(conn, pl.Config) */
	keychan := make(chan []byte)

	keylistener := newKeyListener(keychan)
	pl.Config.KeyLogWriter = keylistener
	pl.Config.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}

	lc := newLinkstConn(conn)
	keyconn := tls.Client(lc, pl.Config)

	err := keyconn.Handshake()
	if err != nil {
		return err
	}
	fmt.Printf("Alice: finished handshake: %+v\n", keyconn.ConnectionState())

	select {
	case key := <-keychan:
		copy(link.osSKey[:], key[:KeySize])
		fmt.Printf("Alice's key is set: %x\n", key[:])
	case <-time.After(time.Second * 2):
		return fmt.Errorf("Key establishment timed out")
	}
	return nil

	/* g, err := dhkx.GetGroup(0)
	if err != nil {
		//log.Fatalf("Could not create group(DH): %v\n", err)
		return err
	}

	//Generate a private key from the group, use the default RNG
	priv, err := g.GeneratePrivateKey(nil)
	if err != nil {
		//log.Fatalf("Could not generate private key(DH): %v\n", err)
		return err
	}

	//Get public key from private key
	pub := priv.Bytes()

	bytes, err := hecomm.NewMessage(hecomm.FPortLinkState, pub)
	if err != nil {
		//log.Fatalf("Could not create hecomm message: %v\n", err)
		return err
	}

	_, err = conn.Write(bytes)
	if err != nil {
		return err
	}

	//Receive bytes from bob, containing bob'pl pub key
	n, err := conn.Read(buf)
	if err != nil {
		//log.Fatalf("Could not read from connection: %v\n", err)
		return err
	}

	message, err := hecomm.GetMessage(buf[:n])
	if err != nil {
		return err
	}
	if message.FPort != hecomm.FPortLinkState {
		return fmt.Errorf("Not the expected fPort != FPortLinkState")
	}

	//Recover Bob'pl public key
	bobPubKey := dhkx.NewPublicKey(message.Data)

	//Compute shared key
	k, err := g.ComputeKey(bobPubKey, priv)
	if err != nil {
		//log.Fatalf("Could not compute key(DH): %v\n", err)
		return err
	}
	//Get the key in []byte form
	key := k.Bytes()
	copy(link.osSKey[:], key[:KeySize])
	return nil */
}

//bob Receiver side form DH perspective, linkstate message already received
func (pl Platform) bob(link *linkType, conn net.Conn, message hecomm.Message) error {
	fmt.Printf("Bob started\n")
	keychan := make(chan []byte)

	keylistener := newKeyListener(keychan)
	pl.Config.KeyLogWriter = keylistener
	pl.Config.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}

	lc := newLinkstConn(conn)
	//_ = tls.Client(lc, pl.Config)
	keyconn := tls.Server(lc, pl.Config)

	err := keyconn.Handshake()
	if err != nil {
		return err
	}
	fmt.Printf("Bob: finished handshake: %+v\n", keyconn.ConnectionState())

	/* var state tls.ConnectionState
	state = keyconn.ConnectionState()
	for state.HandshakeComplete == false {
		time.Sleep(time.Microsecond * 10)
	} */
	fmt.Printf("Handshake completed\n")
	/* err := keyconn.Handshake()
	if err != nil {
		return err
	} */

	select {
	case key := <-keychan:
		copy(link.osSKey[:], key[:KeySize])
		fmt.Printf("Bob's key is set: %x\n", key[:])
	case <-time.After(time.Second * 2):
		return fmt.Errorf("Key establishment timed out")
	}
	return nil
	/* g, err := dhkx.GetGroup(0)
	if err != nil {
		//log.Fatalf("Could not create group(DH): %v\n", err)
		return err
	}

	//Generate a private key from the group, use the default RNG
	priv, err := g.GeneratePrivateKey(nil)
	if err != nil {
		//log.Fatalf("Could not generate private key(DH): %v\n", err)
		return err
	}

	//Get public key from private key
	pub := priv.Bytes()

	//Already received bytes from alice, containing alice'pl pub key
	//Check if right FPort
	if message.FPort != hecomm.FPortLinkState {
		//log.Fatalf("Received message in wrong FPort: %v\n", message)
		return fmt.Errorf("Received message in wrong FPort: %v", message)
	}

	//Create message containig bob'pl pub key
	bytes, err := hecomm.NewMessage(hecomm.FPortLinkState, pub)
	if err != nil {
		//log.Fatalf("Could not create hecomm message: %v\n", err)
		return err
	}

	_, err = conn.Write(bytes)
	if err != nil {
		return err
	}

	//Recover alice'pl public key
	alicePubKey := dhkx.NewPublicKey(message.Data)

	//Compute shared key
	k, err := g.ComputeKey(alicePubKey, priv)
	if err != nil {
		//log.Fatalf("Could not compute key(DH): %v\n", err)
		return err
	}
	//Get the key in []byte form
	key := k.Bytes()
	copy(link.osSKey[:], key[:KeySize])
	return nil */
}
