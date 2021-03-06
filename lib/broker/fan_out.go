/*
Copyright (c) 2014 Ashley Jeffs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package broker

import (
	"sync/atomic"
	"time"

	"github.com/jeffail/benthos/lib/types"
	"github.com/jeffail/util/log"
	"github.com/jeffail/util/metrics"
)

//--------------------------------------------------------------------------------------------------

// FanOut - A broker that implements types.Consumer and broadcasts each message out to an array of
// outputs.
type FanOut struct {
	running int32

	logger log.Modular
	stats  metrics.Type

	messages     <-chan types.Message
	responseChan chan types.Response

	outputMsgChans []chan types.Message
	outputs        []types.Consumer

	closedChan chan struct{}
	closeChan  chan struct{}
}

// NewFanOut - Create a new FanOut type by providing outputs.
func NewFanOut(
	outputs []types.Consumer, logger log.Modular, stats metrics.Type,
) (*FanOut, error) {
	o := &FanOut{
		running:      1,
		stats:        stats,
		logger:       logger.NewModule(".broker.fan_out"),
		messages:     nil,
		responseChan: make(chan types.Response),
		outputs:      outputs,
		closedChan:   make(chan struct{}),
		closeChan:    make(chan struct{}),
	}
	o.outputMsgChans = make([]chan types.Message, len(o.outputs))
	for i := range o.outputMsgChans {
		o.outputMsgChans[i] = make(chan types.Message)
		if err := o.outputs[i].StartReceiving(o.outputMsgChans[i]); err != nil {
			return nil, err
		}
	}
	return o, nil
}

//--------------------------------------------------------------------------------------------------

// StartReceiving - Assigns a new messages channel for the broker to read.
func (o *FanOut) StartReceiving(msgs <-chan types.Message) error {
	if o.messages != nil {
		return types.ErrAlreadyStarted
	}
	o.messages = msgs

	go o.loop()
	return nil
}

//--------------------------------------------------------------------------------------------------

// loop - Internal loop brokers incoming messages to many outputs.
func (o *FanOut) loop() {
	defer func() {
		for _, c := range o.outputMsgChans {
			close(c)
		}
		close(o.responseChan)
		close(o.closedChan)
	}()

	var open bool
	for atomic.LoadInt32(&o.running) == 1 {
		var msg types.Message
		if msg, open = <-o.messages; !open {
			return
		}
		o.stats.Incr("broker.fan_out.messages.received", 1)
		responses := types.NewMappedResponse()
		for i := range o.outputMsgChans {
			select {
			case o.outputMsgChans[i] <- msg:
			case <-o.closeChan:
				return
			}
		}
		var res types.Response
		var nSent int
		for i := range o.outputs {
			var open bool
			select {
			case res, open = <-o.outputs[i].ResponseChan():
				if !open {
					// If any of our outputs is closed then we exit completely. We want to avoid
					// silently starving a particular output.
					o.logger.Warnln("Closing fan_out broker due to closed output")
					return
				}
			case <-o.closeChan:
				return
			}
			if res.Error() != nil {
				responses.Errors[i] = res.Error()
				o.logger.Errorf("Failed to dispatch fan out message: %v\n", res.Error())
				o.stats.Incr("broker.fan_out.output.error", 1)
			} else {
				nSent++
				o.stats.Incr("broker.fan_out.messages.sent", 1)
			}
		}
		if nSent == 0 {
			res = responses
		} else {
			res = types.NewSimpleResponse(nil)
		}
		select {
		case o.responseChan <- res:
		case <-o.closeChan:
			return
		}
	}
}

// ResponseChan - Returns the response channel.
func (o *FanOut) ResponseChan() <-chan types.Response {
	return o.responseChan
}

// CloseAsync - Shuts down the FanOut broker and stops processing requests.
func (o *FanOut) CloseAsync() {
	if atomic.CompareAndSwapInt32(&o.running, 1, 0) {
		close(o.closeChan)
	}
}

// WaitForClose - Blocks until the FanOut broker has closed down.
func (o *FanOut) WaitForClose(timeout time.Duration) error {
	select {
	case <-o.closedChan:
	case <-time.After(timeout):
		return types.ErrTimeout
	}
	return nil
}

//--------------------------------------------------------------------------------------------------
