//go:build !linux

package desktop

import "errors"

type termProc struct{}

func startTerminal(cols, rows int) (*termProc, error) {
	return nil, errors.New("此平台不支持伪终端")
}

func (p *termProc) read(b []byte) (int, error) { return 0, errors.New("不支持") }
func (p *termProc) write(b []byte)             {}
func (p *termProc) resize(cols, rows int)      {}
func (p *termProc) close()                     {}
