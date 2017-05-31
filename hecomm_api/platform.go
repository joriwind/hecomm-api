package hecomm_api

import (
	"context"
	"crypto/tls"
	"log"
	"net"

	"fmt"

	"github.com/joriwind/hecomm-api/hecomm"
	"github.com/monnand/dhkx"
)

const (
	//KeySize Size of the key
	KeySize int = 32
)

//Platform Struct defining the hecomm server
type Platform struct {
	ctx            context.Context
	address        string
	cert           tls.Certificate
	nodes          map[string]*nodeType
	api            PlatformAPI
	fogCredentials FogType
}

//FogType Defines the credentials of the fog
type FogType struct {
	Address string
	Cert    tls.Certificate
	CA      tls.Certificate
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

//PlatformAPI Used to communicate with the IoT platform
type PlatformAPI interface {
	pushKey(deveui []byte, key []byte) error
}

//NewPlatform Create new hecomm server API
func NewPlatform(ctx context.Context, address string, cert tls.Certificate, nodes [][]byte, api PlatformAPI) (*Platform, error) {
	var pl Platform

	pl.ctx = ctx
	pl.address = address
	pl.cert = cert
	for i, val := range nodes {
		pl.nodes[string(nodes[i])] = &nodeType{DevEUI: val}
	}
	pl.api = api

	pl.fogCredentials = FogType{
		Address: "192.168.0.1",
	}

	return &pl, nil
}

//Start Start listening
func (pl *Platform) Start() error {
	config := tls.Config{Certificates: []tls.Certificate{pl.cert}}
	listener, err := tls.Listen("tcp", "localhost:8000", &config)
	if err != nil {
		return err
	}
	defer listener.Close()
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
			if conn.RemoteAddr().String() == pl.fogCredentials.Address {
				pl.handleProviderConnection(conn)
			} else {
				log.Printf("hecommplatform server: wrong connection: %v\n", conn)
			}
		case <-pl.ctx.Done():
			return nil
		}

	}

}

func (pl *Platform) handleProviderConnection(conn net.Conn) {
	buf := make([]byte, 2048)

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
			node, ok := pl.nodes[string(lc.ProvDevEUI[:])]
			//Check if valid node is found --> node.DevEUI not nil or something
			if !ok {
				log.Printf("hecommplatform server: handleconnection: could not find node\n")
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
			if ok := node.Link.contract.Linked; !ok {
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

		case hecomm.FPortLinkState:
			if link.contract.ProvDevEUI == nil {
				log.Printf("The link protocol has not started yet?: %v\n", link)
			}
			err := bob(&link, conn, *message)
			if err != nil {
				log.Printf("DH protocol error: %v\n", err)
				break
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
			node, ok := pl.nodes[string(lc.ProvDevEUI[:])]
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
			err = pl.api.pushKey(node.DevEUI, link.osSKey[:])
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
			pl.nodes[string(node.DevEUI)].Link = link
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
	config := tls.Config{Certificates: append([]tls.Certificate{pl.fogCredentials.Cert}, pl.cert)}

	conn, err := tls.Dial("tcp", pl.fogCredentials.Address, &config)
	if err != nil {
		return err
	}
	defer conn.Close()

	buf := make([]byte, 2048)

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
		if err != nil {
			return err
		}
		//Decode response
		message, err := hecomm.GetMessage(buf[:n])
		if err != nil {
			return err
		}

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
			err = alice(&link, conn, buf)
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
				pl.nodes[string(link.contract.ReqDevEUI[:])].Link = link
				//Push the key down to the fog
				err := pl.api.pushKey(link.contract.ReqDevEUI, link.osSKey[:])
				if err != nil {
					return err
				}
			} else {
				return fmt.Errorf("Hecomm protocol SET response failed")
			}
		default:
			return fmt.Errorf("Unkown or unsupported FPORT: %v", message.FPort)
		}

	}
}

//alice Initiator side from DH perspective
func alice(link *linkType, conn net.Conn, buf []byte) error {
	//Get default group
	g, err := dhkx.GetGroup(0)
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

	//Recover Bob'pl public key
	bobPubKey := dhkx.NewPublicKey(buf[:n])

	//Compute shared key
	k, err := g.ComputeKey(bobPubKey, priv)
	if err != nil {
		//log.Fatalf("Could not compute key(DH): %v\n", err)
		return err
	}
	//Get the key in []byte form
	key := k.Bytes()
	copy(link.osSKey[:], key[:KeySize])
	return nil
}

//bob Receiver side form DH perspective, linkstate message already received
func bob(link *linkType, conn net.Conn, message hecomm.Message) error {
	g, err := dhkx.GetGroup(0)
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
	return nil
}
