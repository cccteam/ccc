package resource

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/go-playground/errors/v5"
)

// A method chooses the status of each response (decided 2026-09-08). The
// statuses it may answer with are declared on the struct with @answers; the
// result type carries HTTPStatus() int and picks one per response. A 2xx is the
// answer, written with the chosen code; a 4xx is a refusal that travels with the
// typed body, and in the transaction form the transaction rolls back first, so
// nothing the body armed commits. 401, 403, and 404 stay the frame's own
// refusals and cannot be declared.

// Answerer is what a method's result type declares to choose the status of
// each response.
type Answerer interface {
	HTTPStatus() int
}

// ResponseStatus is the status a generated frame writes a method's answer
// with: the result's own choice, checked against the statuses the method
// declared. An undeclared status is a programming error; the frame answers 500
// with it, naming the method and the code. A result that does not choose
// answers 200.
func ResponseStatus(method string, result any, declared ...int) (int, error) {
	answerer, ok := result.(Answerer)
	if !ok {
		return http.StatusOK, nil
	}
	status := answerer.HTTPStatus()
	if !slices.Contains(declared, status) {
		return 0, errors.Newf("%s answered with status %d, which its @answers does not declare (declared: %v)", method, status, declared)
	}

	return status, nil
}

// Answer is the sentinel a generated frame returns from its transaction
// function when a method's answer is a refusal (a 4xx): the transaction rolls
// back, nothing retries, and the frame writes the status with the typed body it
// captured. It never reaches the wire as an error.
type Answer struct {
	Status int
}

// Error names the refusal; the frame never writes it.
func (a *Answer) Error() string {
	return fmt.Sprintf("answer %d: the transaction rolls back by design", a.Status)
}

// Refused reports whether a transaction ended by an Answer, and with which
// status.
func Refused(err error) (int, bool) {
	var answer *Answer
	if errors.As(err, &answer) {
		return answer.Status, true
	}

	return 0, false
}
