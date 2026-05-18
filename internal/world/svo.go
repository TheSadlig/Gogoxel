package world

type SVO struct {
	Root SVONode

	Nodes []SVONode
}

type SVONode struct {
	ChildMaskAndColor uint32 // Bits 0-7: Child mask // Bit 8-31: color index
	ChildPointer      uint32 // Index of the first child in the global array
}

func NewSVO() *SVO {
	return &SVO{
		Root:  SVONode{},
		Nodes: []SVONode{},
	}
}
