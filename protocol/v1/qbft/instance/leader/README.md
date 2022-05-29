# iBFT round leader selection

A leader can be selected in many ways, we've implemented a simple deterministic leader selection based on a provided seed for each instance, from which the first leader is selected.

Each round the following operator id is selected in a round-robin fashion.