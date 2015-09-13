// Copyright © 2015 Steve Francia <spf@spf13.com>.
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tbroyer/golangchallenge-gc6/mazelib"
)

// Defining the icarus command.
// This will be called as 'laybrinth icarus'
var icarusCmd = &cobra.Command{
	Use:     "icarus",
	Aliases: []string{"client"},
	Short:   "Start the laybrinth solver",
	Long: `Icarus wakes up to find himself in the middle of a Labyrinth.
  Due to the darkness of the Labyrinth he can only see his immediate cell and if
  there is a wall or not to the top, right, bottom and left. He takes one step
  and then can discover if his new cell has walls on each of the four sides.

  Icarus can connect to a Daedalus and solve many laybrinths at a time.`,
	Run: func(cmd *cobra.Command, args []string) {
		RunIcarus()
	},
}

func init() {
	RootCmd.AddCommand(icarusCmd)
}

func RunIcarus() {
	// Run the solver as many times as the user desires.
	fmt.Println("Solving", viper.GetInt("times"), "times")
	for x := 0; x < viper.GetInt("times"); x++ {

		solveMaze()
	}

	// Once we have solved the maze the required times, tell daedalus we are done
	makeRequest("http://127.0.0.1:" + viper.GetString("port") + "/done")
}

// Make a call to the laybrinth server (daedalus) that icarus is ready to wake up
func awake() mazelib.Survey {
	contents, err := makeRequest("http://127.0.0.1:" + viper.GetString("port") + "/awake")
	if err != nil {
		fmt.Println(err)
	}
	r := ToReply(contents)
	return r.Survey
}

// Make a call to the laybrinth server (daedalus)
// to move Icarus a given direction
// Will be used heavily by solveMaze
func Move(direction string) (mazelib.Survey, error) {
	if direction == "left" || direction == "right" || direction == "up" || direction == "down" {

		contents, err := makeRequest("http://127.0.0.1:" + viper.GetString("port") + "/move/" + direction)
		if err != nil {
			return mazelib.Survey{}, err
		}

		rep := ToReply(contents)
		if rep.Victory == true {
			fmt.Println(rep.Message)
			// os.Exit(1)
			return rep.Survey, mazelib.ErrVictory
		} else {
			return rep.Survey, errors.New(rep.Message)
		}
	}

	return mazelib.Survey{}, errors.New("invalid direction")
}

// utility function to wrap making requests to the daedalus server
func makeRequest(url string) ([]byte, error) {
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return contents, nil
}

// Handling a JSON response and unmarshalling it into a reply struct
func ToReply(in []byte) mazelib.Reply {
	res := &mazelib.Reply{}
	json.Unmarshal(in, &res)
	return *res
}

func solveMaze() {
	s := awake()

	// We want to find one solution in the fewest possible steps, while being
	// "blind" about the maze. This means we need an algorithm focused on "us"
	// (i.e. not the maze) working from inside the maze and fast.
	// So let's use Trémaux's algorithm.

	// We don't know our exact coordinates in the maze, so start at 0,0 and go positive and negative
	solver := &solver{
		junctions:     make(map[mazelib.Coordinate]bool),
		horizPassages: make(map[mazelib.Coordinate]int),
		vertPassages:  make(map[mazelib.Coordinate]int),
	}
	// mark the current coordinate (0,0) as visited
	solver.junctions[solver.coord] = true
	for true {
		// Choose a new passage at random
		dir := solver.chooseDir(s)
		var err error
		s, err = solver.moveDir(s, dir)
		if err == mazelib.ErrVictory {
			return
		}
	}
}

type solver struct {
	coord mazelib.Coordinate // current coordinates
	// we need to track both visited junctions and visited passages.
	junctions     map[mazelib.Coordinate]bool // visited junctions
	horizPassages map[mazelib.Coordinate]int  // passages to the right of the coordinate
	vertPassages  map[mazelib.Coordinate]int  // passages to the bottom of the coordinate
}

func (s *solver) moveDir(sv mazelib.Survey, dir int) (mazelib.Survey, error) {
	var newCoord, passage mazelib.Coordinate
	var passageMap map[mazelib.Coordinate]int
	var dirStr string

	switch dir {
	case mazelib.N:
		newCoord = mazelib.Coordinate{s.coord.X, s.coord.Y - 1}
		passage = newCoord
		passageMap = s.vertPassages
		dirStr = "up"
	case mazelib.S:
		newCoord = mazelib.Coordinate{s.coord.X, s.coord.Y + 1}
		passage = s.coord
		passageMap = s.vertPassages
		dirStr = "down"
	case mazelib.E:
		newCoord = mazelib.Coordinate{s.coord.X + 1, s.coord.Y}
		passage = s.coord
		passageMap = s.horizPassages
		dirStr = "right"
	case mazelib.W:
		newCoord = mazelib.Coordinate{s.coord.X - 1, s.coord.Y}
		passage = newCoord
		passageMap = s.horizPassages
		dirStr = "left"
	default:
		panic("dir out of range")
	}

	// "If you're walking down a new passage and encounter a junction you have
	// visited before, treat it like a dead end and go back the way you came."
	if passageMap[passage] == 0 && s.junctions[newCoord] {
		// Use our knowledge of the maze, this saves us 2 moves on the server
		passageMap[passage] += 2
		return sv, nil
	}

	s.coord = newCoord
	s.junctions[newCoord] = true
	passageMap[passage]++
	return Move(dirStr)
}

func (s *solver) chooseDir(sv mazelib.Survey) int {
	for w := 0; true; w++ {
		d, err := s.chooseDirWeighted(sv, w)
		if err == nil {
			return d
		}
	}
	panic("fully closed cell?")
}

func (s *solver) chooseDirWeighted(sv mazelib.Survey, w int) (int, error) {
	for d := range rand.Perm(4) {
		var wall bool
		var passage int

		dir := d + 1
		switch dir {
		case mazelib.N:
			wall = sv.Top
			passage = s.vertPassages[mazelib.Coordinate{s.coord.X, s.coord.Y - 1}]
		case mazelib.S:
			wall = sv.Bottom
			passage = s.vertPassages[s.coord]
		case mazelib.E:
			wall = sv.Right
			passage = s.horizPassages[s.coord]
		case mazelib.W:
			wall = sv.Left
			passage = s.horizPassages[mazelib.Coordinate{s.coord.X - 1, s.coord.Y}]
		}

		if !wall && passage <= w {
			return dir, nil
		}
	}

	return 0, errors.New("not found")
}
