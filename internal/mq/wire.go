package mq

import "github.com/google/wire"

var Set = wire.NewSet(
	NewMessageQueue,
	wire.Bind(new(MessageQueue), new(*messageQueue)),
)
