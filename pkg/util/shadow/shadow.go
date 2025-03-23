package shadow

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/GehirnInc/crypt"
	_ "github.com/GehirnInc/crypt/sha256_crypt"
	_ "github.com/GehirnInc/crypt/sha512_crypt"
)

const secsInDay = 86400 //int64(24*time.Hour/time.Second)
const ShadowFile = "/etc/shadow"

var (
	ErrNoSuchUser    = errors.New("shadow: user entry is not present in database")
	ErrWrongPassword = errors.New("shadow: wrong password")
)

type Shadow struct {
	shadowFile string
	entries    []Entry
}

func New() *Shadow {
	return &Shadow{
		shadowFile: ShadowFile,
		entries:    []Entry{},
	}
}

func (s *Shadow) Read() error {
	var err error
	s.entries, err = Read(s.shadowFile)
	return err
}

func (s *Shadow) ReadFile(shadowFile string) error {
	var err error
	s.shadowFile = shadowFile
	s.entries, err = Read(s.shadowFile)
	return err
}

func (s *Shadow) Lookup(name string) (*Entry, error) {
	for _, entry := range s.entries {
		if entry.Name == name {
			return &entry, nil
		}
	}

	return nil, ErrNoSuchUser
}

type Entry struct {
	// User login name.
	Name string

	// Hashed user password.
	Pass string

	// Days since Jan 1, 1970 password was last changed.
	LastChange int

	// The number of days the user will have to wait before she will be allowed to
	// change her password again.
	//
	// -1 if password aging is disabled.
	MinPassAge int

	// The number of days after which the user will have to change her password.
	//
	// -1 is password aging is disabled.
	MaxPassAge int

	// The number of days before a password is going to expire (see the maximum
	// password age above) during which the user should be warned.
	//
	// -1 is password aging is disabled.
	WarnPeriod int

	// The number of days after a password has expired (see the maximum
	// password age above) during which the password should still be accepted.
	//
	// -1 is password aging is disabled.
	InactivityPeriod int

	// The date of expiration of the account, expressed as the number of days
	// since Jan 1, 1970.
	//
	// -1 is account never expires.
	AcctExpiry int

	// Unused now.
	Flags int
}

func (e *Entry) IsAccountValid() bool {
	if e.AcctExpiry == -1 {
		return true
	}

	nowDays := int(time.Now().Unix() / secsInDay)
	return nowDays < e.AcctExpiry
}

func (e *Entry) IsPasswordValid() bool {
	if e.LastChange == -1 || e.MaxPassAge == -1 || e.InactivityPeriod == -1 {
		return true
	}

	nowDays := int(time.Now().Unix() / secsInDay)
	return nowDays < e.LastChange+e.MaxPassAge+e.InactivityPeriod
}

func (e *Entry) VerifyPassword(pass string) (err error) {
	// Do not permit null and locked passwords.
	if e.Pass == "" {
		return errors.New("verify: null password")
	}
	if e.Pass[0] == '!' {
		return errors.New("verify: locked password")
	}

	// crypt.NewFromHash may panic on unknown hash function.
	defer func() {
		if rcvr := recover(); rcvr != nil {
			err = fmt.Errorf("%v", rcvr)
		}
	}()

	if err := crypt.NewFromHash(e.Pass).Verify(e.Pass, []byte(pass)); err != nil {
		if errors.Is(err, crypt.ErrKeyMismatch) {
			return ErrWrongPassword
		}
		return err
	}
	return nil
}

// Read reads system shadow passwords database and returns all entires in it.
func Read(shadowFile string) ([]Entry, error) {
	f, err := os.Open(shadowFile)
	if err != nil {
		return nil, err
	}
	s := bufio.NewScanner(f)

	var res []Entry
	for s.Scan() {
		ent, err := parseEntry(s.Text())
		if err != nil {
			return res, err
		}

		res = append(res, *ent)
	}
	if err := s.Err(); err != nil {
		return res, err
	}
	return res, nil
}

func parseEntry(line string) (*Entry, error) {
	parts := strings.Split(line, ":")
	if len(parts) != 9 {
		return nil, errors.New("read: malformed entry")
	}

	res := &Entry{
		Name: parts[0],
		Pass: parts[1],
	}

	for i, value := range [...]*int{
		&res.LastChange, &res.MinPassAge, &res.MaxPassAge,
		&res.WarnPeriod, &res.InactivityPeriod, &res.AcctExpiry, &res.Flags,
	} {
		if parts[2+i] == "" {
			*value = -1
		} else {
			var err error
			*value, err = strconv.Atoi(parts[2+i])
			if err != nil {
				return nil, fmt.Errorf("read: invalid value for field %d", 2+i)
			}
		}
	}

	return res, nil
}

func Lookup(name string) (*Entry, error) {
	return LookupFile(name, ShadowFile)
}

func LookupFile(name, shadowFile string) (*Entry, error) {
	entries, err := Read(shadowFile)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Name == name {
			return &entry, nil
		}
	}

	return nil, ErrNoSuchUser
}
