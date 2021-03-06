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

package input

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jeffail/benthos/lib/types"
	"github.com/jeffail/util/log"
	"github.com/jeffail/util/metrics"
)

func TestHTTPBasic(t *testing.T) {
	nTestLoops := 1000

	conf := NewConfig()
	conf.HTTPServer.Address = "localhost:1237"
	conf.HTTPServer.Path = "/testpost"

	h, err := NewHTTPServer(conf, log.NewLogger(os.Stdout, logConfig), metrics.DudType{})
	if err != nil {
		t.Error(err)
		return
	}

	resChan := make(chan types.Response)

	if err = h.StartListening(resChan); err != nil {
		t.Error(err)
		return
	}

	<-time.After(time.Second)

	// Test both single and multipart messages.
	for i := 0; i < nTestLoops; i++ {
		testStr := fmt.Sprintf("test%v", i)
		// Send it as single part
		if res, err := http.Post(
			"http://localhost:1237/testpost",
			"application/octet-stream",
			bytes.NewBuffer([]byte(testStr)),
		); err != nil {
			t.Error(err)
			return
		} else if res.StatusCode != 200 {
			t.Errorf("Wrong error code returned: %v", res.StatusCode)
			return
		}
		select {
		case resMsg := <-h.MessageChan():
			if res := string(resMsg.Parts[0]); res != testStr {
				t.Errorf("Wrong result, %v != %v", resMsg, res)
			}
		case <-time.After(time.Second):
			t.Error("Timed out waiting for message")
		}
		select {
		case resChan <- types.NewSimpleResponse(nil):
		case <-time.After(time.Second):
			t.Error("Timed out waiting for response")
		}
	}

	// Test MIME multipart parsing, as defined in RFC 2046
	for i := 0; i < nTestLoops; i++ {
		partOne := fmt.Sprintf("test%v part one", i)
		partTwo := fmt.Sprintf("test%v part two", i)

		testStr := fmt.Sprintf(
			"--foo\r\n"+
				"Content-Type: application/octet-stream\r\n\r\n"+
				"%v\r\n"+
				"--foo\r\n"+
				"Content-Type: application/octet-stream\r\n\r\n"+
				"%v\r\n"+
				"--foo--\r\n",
			partOne, partTwo)

		// Send it as multi part
		if res, err := http.Post(
			"http://localhost:1237/testpost",
			"multipart/mixed; boundary=foo",
			bytes.NewBuffer([]byte(testStr)),
		); err != nil {
			t.Error(err)
			return
		} else if res.StatusCode != 200 {
			t.Errorf("Wrong error code returned: %v", res.StatusCode)
			return
		}
		select {
		case resMsg := <-h.MessageChan():
			if exp, actual := 2, len(resMsg.Parts); exp != actual {
				t.Errorf("Wrong number of parts: %v != %v", actual, exp)
			} else if exp, actual := partOne, string(resMsg.Parts[0]); exp != actual {
				t.Errorf("Wrong result, %v != %v", actual, exp)
			} else if exp, actual := partTwo, string(resMsg.Parts[1]); exp != actual {
				t.Errorf("Wrong result, %v != %v", actual, exp)
			}
		case <-time.After(time.Second):
			t.Error("Timed out waiting for message")
		}
		select {
		case resChan <- types.NewSimpleResponse(nil):
		case <-time.After(time.Second):
			t.Error("Timed out waiting for response")
		}
	}
}
