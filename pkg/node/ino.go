package node

// InodeNumbersGenerator generates inode numbers starting from 2 (1 is reserved for the root node)
type InodeNumbersGenerator struct {
	nextCh chan uint64
	stopCh chan struct{}
}

// NewInodeNumbersGenerator creates a new inode generator
func NewInodeNumbersGenerator() *InodeNumbersGenerator {
	g := &InodeNumbersGenerator{
		// Keep a buffer of ready to consume inode numbers.
		nextCh: make(chan uint64, 42),
		stopCh: make(chan struct{}),
	}
	return g
}

// Start starts the generator
func (g *InodeNumbersGenerator) Start() {
	// 1 is reserved for the root.
	var ino uint64 = 2
	for {
		select {
		case g.nextCh <- ino:
			// No check. I don't think we'll run out if inode numbers.
			ino++
		case <-g.stopCh:
			return
		}
	}
}

// Stop stops the generator
func (g *InodeNumbersGenerator) Stop() {
	close(g.stopCh)
}

// Next returns the next generated inode
func (g *InodeNumbersGenerator) Next() uint64 {
	return <-g.nextCh
}
