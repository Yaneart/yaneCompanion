package torrent

import (
	"errors"
	"github.com/anacrolix/torrent/companionguard/locator"
	"github.com/anacrolix/torrent/metainfo"
	"testing"
)

func TestCompanionRejectsUnsafeMetadataBeforeStorage(t *testing.T) {
	for _, data := range [][]byte{[]byte("de"), []byte("d4:name6:../bade"), []byte("d12:meta versioni2ee")} {
		torrent := &Torrent{infoHash: metainfo.HashBytes(data)}
		if err := torrent.setInfoBytes(data); !errors.Is(err, locator.ErrUnsafe) {
			t.Fatal("unsafe metadata reached engine storage", err)
		}
	}
	torrent := new(Torrent)
	if torrent.setMetadataSize(locator.MaxBytes+1) == nil {
		t.Fatal("unbounded peer metadata accepted")
	}
}
