// Copyright 2017 The Go Authors. All rights reserved.
// Copyright 2024 Solar Designer. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// yescrypt support sponsored by Sandfly Security https://sandflysecurity.com -
// Agentless Security for Linux

package yescrypt_test

import (
	"encoding/base64"
	"fmt"
	"log"

	"ssh2incus/pkg/yescrypt"
)

func Example() {
	// DO NOT use this salt value; generate your own random salt. 8 bytes is
	// a good length.
	salt := []byte{0xc8, 0x28, 0xf2, 0x58, 0xa7, 0x6a, 0xad, 0x7b}

	dk, err := yescrypt.ScryptKey([]byte("some password"), salt, 1<<15, 8, 1, 32)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(base64.StdEncoding.EncodeToString(dk))
	hash, err := yescrypt.Hash([]byte("openwall"), []byte("$y$j9T$AAt9R641xPvCI9nXw1HHW/"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(hash))
	hash, err = yescrypt.Hash([]byte("pleaseletmein"), []byte("$y$j9T$e8R9q85ZuzUkArEUurdtS.$esON.7y6H.u3UCPVCpbRFueRpAut2n2cMf1EhpjbuiC"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(hash))
	// Output: lGnMz8io0AUkfzn6Pls1qX20Vs7PGN6sbYQ2TQgY12M=
	// $y$j9T$AAt9R641xPvCI9nXw1HHW/$cuQRBMN3N/f8IcmVN.4YrZ1bHMOiLOoz9/XQMKV/v0A
	// $y$j9T$e8R9q85ZuzUkArEUurdtS.$esON.7y6H.u3UCPVCpbRFueRpAut2n2cMf1EhpjbuiC
}
