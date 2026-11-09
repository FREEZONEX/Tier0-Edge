package streamexec

import "time"

// Transport allowance is separate from each execution's enforced deadline.
const MaxExecutorTransportTimeout = time.Duration(MaxProgramTimeoutMs)*time.Millisecond + 2*time.Second

// Program source calls have a separate ceiling; legacy reads retain 25 seconds.
const MaxProgramSourceTimeoutMs = 45000
const MaxProgramSourceTimeout = time.Duration(MaxProgramSourceTimeoutMs) * time.Millisecond

// MaxProgramDataBytes bounds each source response and the complete v2 program
// result. Request inputs and legacy transforms retain MaxJSONBytes.
const MaxProgramDataBytes = 8 * 1024 * 1024

// Envelopes include code and transport metadata. Each exchange is checked
// separately and the complete session also has a finite byte budget.
const MaxExchangeBytes = MaxProgramDataBytes + MaxCodeBytes + 64*1024

// The per-direction session budget is independent of the call-count ceiling:
// many maximum-sized replies must not multiply into hundreds of MiB.
const MaxProgramExchangeBytes = 32 * 1024 * 1024
