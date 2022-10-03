package errors_test

//import (
//	"fmt"
//
//	"github.com/incident-io/core/server/pkg/errors"
//
//	. "github.com/onsi/ginkgo"
//	. "github.com/onsi/gomega"
//)
//
//func getStackTraces(err error) []errors.StackTrace {
//	traces := []errors.StackTrace{}
//	if err, ok := err.(errors.StackTracer); ok {
//		traces = append(traces, err.StackTrace())
//	}
//
//	if err := errors.Unwrap(err); err != nil {
//		traces = append(traces, getStackTraces(err)...)
//	}
//
//	return traces
//}
//
//var _ = Describe("errors", func() {
//	Describe("New", func() {
//		It("generates an error with a stack trace", func() {
//			err := errors.New("oops")
//			Expect(getStackTraces(err)).To(HaveLen(1))
//		})
//	})
//
//	Describe("Wrap", func() {
//		Context("when cause has no stack trace", func() {
//			It("wraps the error and takes stack trace", func() {
//				err := errors.Wrap(fmt.Errorf("cause"), "description")
//				Expect(err.Error()).To(Equal("description: cause"))
//
//				cause := errors.Cause(err)
//				Expect(cause).To(MatchError("cause"))
//
//				Expect(getStackTraces(err)).To(HaveLen(1))
//			})
//		})
//
//		Context("when cause has stack trace", func() {
//			Context("which is not an ancestor of our own", func() {
//				It("creates a new stack trace", func() {
//					errChan := make(chan error)
//					go func() {
//						errChan <- errors.New("unrelated") // created with a stack trace
//					}()
//
//					err := errors.Wrap(<-errChan, "helpful description")
//					Expect(err.Error()).To(Equal("helpful description: unrelated"))
//
//					Expect(getStackTraces(err)).To(HaveLen(2))
//				})
//			})
//
//			Context("with a frame from our current method", func() {
//				It("does not create new stack trace", func() {
//					err := errors.Wrap(errors.New("related"), "helpful description")
//					Expect(err.Error()).To(Equal("helpful description: related"))
//
//					Expect(getStackTraces(err)).To(HaveLen(1))
//				})
//			})
//		})
//	})
//})
