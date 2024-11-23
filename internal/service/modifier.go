package service

import "github.com/asticode/go-astisub"

type ModifyService interface {
	ModifyLine(item *astisub.Item)
}

type modifyService struct{}

func NewModifyService() *modifyService {
	return &modifyService{}
}

func (m *modifyService) ModifyLine(item *astisub.Item) {

}
