package chatadvisor

import (
	"context"
	"strings"

	"charm.land/fantasy"
	"golang.org/x/xerrors"

	stringutil "github.com/coder/coder/v2/coderd/util/strings"
	"github.com/coder/coder/v2/coderd/x/chatd/chatnested"
)

// RunAdvisorOptions carries optional streaming callbacks for a
// single RunAdvisor invocation.
type RunAdvisorOptions struct {
	OnAdviceDelta func(delta string)
	OnAdviceReset func()
}

// RunAdvisor executes a single, tool-less nested advisor call.
func (rt *Runtime) RunAdvisor(
	ctx context.Context,
	question string,
	conversationSnapshot []fantasy.Message,
	opts *RunAdvisorOptions,
) (AdvisorResult, error) {
	// Model, MaxUsesPerRun, and MaxOutputTokens are validated by NewRuntime.
	// Runtime fields are unexported so callers cannot bypass that.
	question = strings.TrimSpace(question)
	if question == "" {
		return AdvisorResult{}, xerrors.New("advisor question is required")
	}
	question = stringutil.Truncate(question, advisorQuestionMaxRunes)

	if !rt.tryAcquire() {
		return AdvisorResult{
			Type:          ResultTypeLimitReached,
			RemainingUses: 0,
		}, nil
	}

	runOpts := chatnested.RunTextOptions{
		Model:           rt.cfg.Model,
		Messages:        BuildAdvisorMessages(question, conversationSnapshot),
		ModelConfig:     rt.cfg.ModelConfig,
		ProviderOptions: rt.cfg.ProviderOptions,
	}
	if opts != nil {
		runOpts.OnTextDelta = opts.OnAdviceDelta
		runOpts.OnTextReset = opts.OnAdviceReset
	}

	runResult, err := chatnested.RunText(ctx, runOpts)
	if err != nil {
		// Refund the use so a transient provider failure does not
		// permanently exhaust the per-run advisor budget.
		rt.release()
		return AdvisorResult{
			Type:          ResultTypeError,
			Error:         err.Error(),
			RemainingUses: rt.RemainingUses(),
		}, nil
	}

	advice := runResult.Text
	if advice == "" {
		// Refund: the run did not produce advice, so the contract
		// "increments on every successful advisor call" treats this
		// as not consuming a use.
		rt.release()
		return AdvisorResult{
			Type:          ResultTypeError,
			Error:         "advisor produced no text output",
			RemainingUses: rt.RemainingUses(),
		}, nil
	}

	return AdvisorResult{
		Type:          ResultTypeAdvice,
		Advice:        advice,
		AdvisorModel:  rt.cfg.Model.Provider() + "/" + rt.cfg.Model.Model(),
		RemainingUses: rt.RemainingUses(),
	}, nil
}
