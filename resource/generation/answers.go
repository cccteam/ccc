package generation

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// A method chooses its status per response (decided 2026-09-08). @answers on
// the struct declares the statuses it may answer with, and the result type
// carries HTTPStatus() int to pick one; the two go together, and generation
// refuses one without the other, naming the struct. An answerless method may
// declare @answers(204) alone, meaning 204 instead of 200: the one static case.

// answerStatusAllowed reports whether a status may be declared: 200, 201, 202,
// 204, and the 4xx range less 401, 403, and 404, which stay the frame's own
// refusals.
func answerStatusAllowed(status int) bool {
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return true
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false
	default:
		return status >= http.StatusBadRequest && status < http.StatusInternalServerError
	}
}

// resolveAnswers reads and validates the struct's @answers against what its
// Execute answers with.
func resolveAnswers(rpcMethod *rpcMethodInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if !annotations.Struct.Has(answersKeyword) {
		if rpcMethod.choosesStatus {
			return errors.Newf("struct %s: the result type %s declares HTTPStatus() int but the method declares no @%s; declare the statuses it may answer with, for example @%s(200, 409)", pStruct.Name(), rpcMethod.Result.Source, answersKeyword, answersKeyword)
		}

		return nil
	}

	statuses, err := parseAnswers(annotations.Struct.Get(answersKeyword))
	if err != nil {
		return errors.Wrapf(err, "struct %s", pStruct.Name())
	}

	if !rpcMethod.Answers() {
		if len(statuses) != 1 || statuses[0] != http.StatusNoContent {
			return errors.Newf("struct %s: @%s(%s) on a method whose Execute returns error alone; without a result to choose it, the only status such a method may declare is 204", pStruct.Name(), answersKeyword, statusList(statuses, ", "))
		}
		rpcMethod.Statuses = statuses

		return nil
	}

	if !rpcMethod.choosesStatus {
		return errors.Newf("struct %s: @%s is declared but the result type %s has no HTTPStatus() int method to choose the status per response", pStruct.Name(), answersKeyword, rpcMethod.Result.Source)
	}
	if slices.Contains(statuses, http.StatusNoContent) && !rpcMethod.ResultPointer {
		return errors.Newf("struct %s: @%s declares 204, which writes no body, but Execute answers with a %s value; answer with *%s and return nil for 204", pStruct.Name(), answersKeyword, rpcMethod.Result.Source, rpcMethod.Result.Source)
	}
	rpcMethod.Statuses = statuses

	return nil
}

// parseAnswers reads the declared statuses: integers, each allowed, none
// repeated, at least one of them a success.
func parseAnswers(arg genlang.Arg) ([]int, error) {
	var statuses []int
	for invocation := range arg.Seq() {
		for part := range strings.SplitSeq(invocation, ",") {
			text := strings.TrimSpace(part)
			status, err := strconv.Atoi(text)
			if err != nil {
				return nil, errors.Newf("@%s(%s): %q is not an HTTP status code", answersKeyword, invocation, text)
			}
			if !answerStatusAllowed(status) {
				return nil, errors.Newf("@%s(%s): %d may not be declared; a method answers with 200, 201, 202, 204, or a 4xx other than 401, 403, and 404, which are the frame's own refusals", answersKeyword, invocation, status)
			}
			if slices.Contains(statuses, status) {
				return nil, errors.Newf("@%s(%s) declares %d twice", answersKeyword, invocation, status)
			}
			statuses = append(statuses, status)
		}
	}
	if !slices.ContainsFunc(statuses, func(status int) bool { return status < http.StatusBadRequest }) {
		return nil, errors.Newf("@%s(%s) declares no success status; a method must be able to answer with a 2xx", answersKeyword, statusList(statuses, ", "))
	}

	return statuses, nil
}
