# Doppelganger Protection - Design Overview

This document provides an overview of the Doppelganger Protection mechanism in `ssv.network`.

🔗 **Full Documentation**: [Doppelganger Protection Design on HackMD](https://hackmd.io/qwoDR80_Q6yBQVA6kt_hpg?view)

## Summary
Doppelganger Protection ensures that validators do not accidentally sign while active elsewhere, preventing slashing risks.

**Key Features:**
- Uses **validator liveness tracking** to determine whether a validator is already signing on the Ethereum network.
- Implements **post-consensus quorum-based safety** to allow signing when enough confirmations exist.
- Operates within the **distributed validator model** of `ssv.network`, ensuring protection even in multi-operator environments.

**For full details, refer to the HackMD link above.**
