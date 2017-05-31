package hecomm_api

import (
	"context"
	"crypto/tls"
	"log"
	"net"

	"fmt"

	"github.com/monnand/dhkx"
)

const (
	//KeySize Size of the key
	KeySize int = 32
)

//Platform Struct defining the hecomm server
type Platform struct {
	ctx         context.Context
	address     string
	credentials tls.Certificate
}

type fog struct{
	Address string
	TlsConfig tls.Config
}
const (
	FogCredentials fog = fog{
		Address: "192.168.0.1",
		TlsConfig: tls.Config{

		},
	}
)

//Link A link between node and the contract
type Link struct {
	linkContract hecomm.LinkContract
	osSKey       [KeySize]byte
}

//NewPlatform Create new hecomm server API
func NewPlatform(ctx context.Context, address string, credentials tls.Certificate) (*Platform, error) {
	var srv Platform

	srv.ctx = ctx
	srv.address = address
	srv.credentials = credentials

	return &srv, nil
}

//Start Start listening
func (pl *Platform) Start() error {
	config := tls.Config{Certificates: []tls.Certificate{pl.credentials}}
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
			if conn.RemoteAddr().String() == FogCredentials.Address {
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

	//Get the storage pointer
	var store *storage.Storage
	store, ok := pl.ctx.Value(main.keyStorageID).(*storage.Storage)
	if !ok {
		log.Fatalf("Could not assert storage!: %v", pl.ctx.Value("storage"))
	}

	//The to be created link
	var link Link
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

			//Convert eui
			eui := ConvertHecommDevEUIToPlatformDevEUI(lc.ProvDevEUI)

			//Find possible node
			//TODO: locate node
			node, ok := store.GetNode(eui)
			if !ok {
				log.Fatalf("hecommplatform server: handleConnection: could not resolve deveui: %v\n", err)
				return
			}
			//Check if valid node is found --> node.DevEUI not nil or something
			if node.DevEUI != eui {
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
			if ok := node.Link.Linked; !ok {
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
			if link.linkContract.ProvDevEUI != nil {
				log.Printf("Already start linking!?: Link: %v LinkContract: %v\n", link, lc)
				break
			}
			link.linkContract = *lc

		case hecomm.FPortLinkState:
			if link.linkContract.ProvDevEUI == nil {
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

			//Convert identifier
			eui := ConvertHecommDevEUIToPlatformDevEUI(lc.ProvDevEUI)

			//Check corresponds with active linking
			for index, b := range link.linkContract.ProvDevEUI {
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
			node, ok := store.GetNode(eui)
			if !ok {
				log.Printf("get node error: %pl\n", err)

				//Send bad response
				bytes, err := hecomm.NewResponse(false)
				if err != nil {
					log.Fatalf("Could not create hecomm false response: %v\n", err)
				}
				conn.Write(bytes)
				return
			}
			//Add item, pushing key down to node
			err = cisixlowpan.SendCoapRequest(coap.POST, node.Addr, "/key", string(link.osSKey[:])
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
			pl.links[eui] = link
			log.Printf("Link is set: %v\n", link)
			//Clear link
			link = Link{}

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

func (pl *Platform) RequestLink(node storage.Node, infType int){
	config := tls.Config{Certificates: append(FogCredentials.TlsConfig.Certificates, tls.Certificate{pl.credentials})}

	conn, err := tls.Dial("tcp", FogCredentials.Address, &config)
	if err != nil {
		return err
	}
	defer conn.Close()
	
	buf := make([]byte, 2048)

	//Get the storage pointer
	var store *storage.Storage
	store, ok := pl.ctx.Value(main.keyStorageID).(*storage.Storage)
	if !ok {
		log.Fatalf("Could not assert storage!: %v", pl.ctx.Value("storage"))
	}

	//The to be created link
	link := Link{
		linkContract: hecomm.LinkContract{
			InfType: infType,
			ReqDevEUI: []byte(node.DevEUI)
		},
	}
	for {

	}
}

//alice Initiator side from DH perspective
func alice(link *Link, conn net.Conn, buf []byte) error {
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
func bob(link *Link, conn net.Conn, message hecomm.Message) error {
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
