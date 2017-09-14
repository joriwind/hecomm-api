package hecommAPI

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"testing"

	"github.com/joriwind/hecomm-api/hecomm"
)

const (
	infType int = 1111
)

func TestSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ipServer := getLocalIP() + ":8076"
	var nodesServer [][]byte
	nodesServer = append(nodesServer, []byte{0, 0, 0, 0, 0, 0, 0, 1})

	tlscServer := loadCertificate("./certs/6lowpan.cert.pem", "./private/6lowpan.key.pem", "./certs/ca-chain.cert.pem")

	plServer, err := setupPlatform(ctx, ipServer, tlscServer)
	if err != nil {
		t.Errorf("Unable to setup server platform: %v\n", err)
		return
	}

	err = registerNodes(plServer, nodesServer, ipServer, true)
	if err != nil {
		t.Errorf("Unable register server nodes: %v\n", err)
	}

	go func() {
		err := plServer.Start()
		if err != nil {
			log.Fatalf("Hecomm server platform exited with error: %v\n", err)
		} else {
			log.Println("Hecomm server platform server stopped!")
		}
	}()

	ipClient := getLocalIP() + ":8077"
	var nodesClient [][]byte
	nodesClient = append(nodesClient, []byte{0, 0, 0, 0, 0, 0, 0, 2})

	tlscClient := loadCertificate("./certs/lora-app-server.cert.pem", "./private/lora-app-server.key.pem", "./certs/ca-chain.cert.pem")
	plClient, err := setupPlatform(ctx, ipClient, tlscClient)
	if err != nil {
		t.Errorf("Unable to setup client platform: %v\n", err)
	}

	err = registerNodes(plClient, nodesClient, ipClient, false)
	if err != nil {
		t.Errorf("Unable register client nodes: %v\n", err)
	}

	go func() {
		err := plClient.Start()
		if err != nil {
			log.Fatalf("Hecomm client platform exited with error: %v\n", err)
		} else {
			log.Println("Hecomm client platform server stopped!")
		}
	}()

	err = plClient.RequestLink(nodesClient[0], infType)
	if err != nil {
		t.Errorf("Unable to request Link: %v\n", err)
	}
}

func setupPlatform(ctx context.Context, ip string, tlsc *tls.Config) (*Platform, error) {
	var err error
	var pl *Platform

	pl, err = NewPlatform(ctx, ip, tlsc, nil, func(deveui []byte, key []byte) error {
		fmt.Printf("Platform %v: key: %v pushed to node: %v\n", ip, string(key), string(deveui))
		return nil
	})

	dbpl := hecomm.DBCPlatform{
		Address: ip,
		CI:      hecomm.CISixlowpan,
	}

	err = pl.RegisterPlatform(dbpl)
	if err != nil {
		return nil, err
	}

	return pl, nil
}

func registerNodes(pl *Platform, nodes [][]byte, platformIP string, isProvider bool) error {
	var dbn []hecomm.DBCNode
	for _, node := range nodes {
		dbn = append(dbn, hecomm.DBCNode{
			DevEUI:     node,
			InfType:    infType,
			PlAddress:  platformIP,
			IsProvider: isProvider,
			PlType:     hecomm.CISixlowpan,
		})

		pl.AddNode(node)

	}

	err := pl.RegisterNodes(dbn)
	if err != nil {
		return err
	}
	return nil
}

func loadCertificate(pathCert, pathKey, pathCacert string) *tls.Config {
	cert, err := tls.LoadX509KeyPair(pathCert, pathKey)
	if err != nil {
		log.Fatalf("fogcore: tls error: loadkeys: %s", err)
		return nil
	}

	caCert, err := ioutil.ReadFile(pathCacert)
	if err != nil {
		log.Fatalf("cacert error: %v\n", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	config := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		ClientCAs:          caCertPool,
		InsecureSkipVerify: true,
	}
	return config
}

func getLocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("Error in searching localIP: %v\n", err)
		return ""
	}
	// handle err
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			log.Printf("Error in searching localIP: %v\n", err)
			return ""
		}
		// handle err
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			//If it is not loopback, it should be ok
			if !ip.IsLoopback() {

				return ip.String()
			}

		}
	}
	log.Printf("No non loopback IP addresses found!\n")
	return ""
}
