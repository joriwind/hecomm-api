package hecomm

/*
 *	The structs to be used to communicate with the fog implementation - DBCommands
 */

//CIType Type of the iot interface
type CIType int

//Defines the CIType's possible
const (
	CISixlowpan CIType = iota + 1
	CILorawan
)

//DBCPlatform the struct used for passing information about the platform
type DBCPlatform struct {
	Address string
	CI      CIType
}

//DBCNode the struct used to pass information about a node
type DBCNode struct {
	DevEUI     []byte
	PlAddress  string
	PlType     CIType
	IsProvider bool
	InfType    int
}
