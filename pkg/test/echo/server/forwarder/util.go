// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package forwarder

import (
	"bytes"
	"fmt"
	"istio.io/pkg/log"
	"net/http"
	"net/textproto"
)

const (
	hostHeader = "Host"
)

var fwLog = log.RegisterScope("forwarder", "echo clientside", 0)

func writeHeaders(requestID int, header http.Header, outBuffer bytes.Buffer, addFn func(string, string)) {
	for key, values := range header {
		key = textproto.CanonicalMIMEHeaderKey(key)
		for _, v := range values {
			addFn(key, v)
			if key == hostHeader {
				outBuffer.WriteString(fmt.Sprintf("[%d] Host=%s\n", requestID, v))
			} else {
				outBuffer.WriteString(fmt.Sprintf("[%d] Header=%s:%s\n", requestID, key, v))
			}
		}
	}
}
