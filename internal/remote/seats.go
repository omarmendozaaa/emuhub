package remote

// Seat is one player slot of a room. Seats are numbered from player 1 in
// the order the emulator will see them.
type Seat struct {
	// Kind is "host", "guest" or "" while nobody sits there.
	Kind string `json:"kind"`
	// Guest is the guest's number in order of arrival, from 1.
	Guest int `json:"guest"`
}

// MaxSeats is the most players a room deals with.
const MaxSeats = 4

// GuestSeats is how many guests play in a room of that many players.
func GuestSeats(players int, hostPlays bool) int {
	if hostPlays {
		players--
	}
	return min(max(players, 0), MaxGuests)
}

// ClampPlayers fits a player count to what the game's emulator and a room
// allow. A host who plays needs at least one guest.
func ClampPlayers(players int, hostPlays bool, most int) int {
	most = min(most, MaxSeats)
	least := 1
	if hostPlays {
		least = 2
	}
	return max(least, min(players, most))
}

// NewSeats lays out a room: the host, if playing, starts as player 1.
func NewSeats(players int, hostPlays bool) []Seat {
	seats := make([]Seat, players)
	if hostPlays && players > 0 {
		seats[0] = Seat{Kind: "host"}
	}
	return seats
}

// SeatGuest gives a guest the first free seat and reports whether there was
// one.
func SeatGuest(seats []Seat, guest int) bool {
	for i := range seats {
		if seats[i].Kind == "" {
			seats[i] = Seat{Kind: "guest", Guest: guest}
			return true
		}
	}
	return false
}

// MoveSeat swaps a seat with its neighbour, delta being -1 or 1, and returns
// the seat's new position.
func MoveSeat(seats []Seat, i, delta int) int {
	j := i + delta
	if i < 0 || i >= len(seats) || j < 0 || j >= len(seats) {
		return i
	}
	seats[i], seats[j] = seats[j], seats[i]
	return j
}

// PadIndexes tells which gamepad plays in each seat, as the emulator numbers
// them: the host's own pads come first, then one per guest in order of
// arrival. A free seat is promised to the next guest to arrive, so someone
// who joins with the game running can still play. -1 means no gamepad: a
// host without one plays on the keyboard.
func PadIndexes(seats []Seat, hostPads int) []int {
	next := 1
	for _, s := range seats {
		if s.Kind == "guest" && s.Guest >= next {
			next = s.Guest + 1
		}
	}
	out := make([]int, len(seats))
	for i, s := range seats {
		switch s.Kind {
		case "host":
			out[i] = -1
			if hostPads > 0 {
				out[i] = 0
			}
		case "guest":
			out[i] = hostPads + s.Guest - 1
		default:
			out[i] = hostPads + next - 1
			next++
		}
	}
	return out
}
