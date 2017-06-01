package hecommAPI

import (
	"crypto/tls"

	"encoding/json"

	"github.com/joriwind/hecomm-api/hecomm"
)

//FogType Defines the credentials of the fog
type FogType struct {
	Address string
	Cert    tls.Certificate
	CA      tls.Certificate
}

//RegisterNodes Register the nodes in the fog implementation
func RegisterNodes(nodes []hecomm.DBCNode, fog FogType) error {
	config := tls.Config{Certificates: []tls.Certificate{fog.Cert}}

	conn, err := tls.Dial("tcp", fog.Address, &config)
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, node := range nodes {
		nodebytes, err := json.Marshal(node)
		if err != nil {
			return err
		}
		bytes, err := hecomm.NewDBCommand(true, hecomm.ETypeNode, nodebytes)
		if err != nil {
			return err
		}
		bytes, err = hecomm.NewMessage(hecomm.FPortDBCommand, bytes)
		if err != nil {
			return err
		}

		conn.Write(bytes)
	}

	return nil
}

//RegisterPlatform Register the platform in the fog implementation
func RegisterPlatform(pl hecomm.DBCPlatform, fog FogType) error {
	config := tls.Config{Certificates: []tls.Certificate{fog.Cert}}

	conn, err := tls.Dial("tcp", fog.Address, &config)
	if err != nil {
		return err
	}
	defer conn.Close()

	plbytes, err := json.Marshal(pl)
	if err != nil {
		return err
	}
	bytes, err := hecomm.NewDBCommand(true, hecomm.ETypePlatform, plbytes)
	if err != nil {
		return err
	}
	bytes, err = hecomm.NewMessage(hecomm.FPortDBCommand, bytes)
	if err != nil {
		return err
	}

	conn.Write(bytes)

	return nil
}
