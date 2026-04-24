# **2\. System Backbone (from your docs, enforced)**

* DTO-only communication  
* event bus (append-only)  
* worker-based execution (`SELECT … SKIP LOCKED`)  
* per-market isolation (crypto module only here)  
* Telegram via event bus (no direct calls)

---

This backbone is the **mechanical foundation** that guarantees determinism, scalability, and safe learning. If this is wrong, your control loop becomes non-reproducible and your learning becomes garbage.

We’ll go deep into each constraint and how it must be implemented.

---

# **DTO-ONLY COMMUNICATION**

## **Definition**

All modules communicate via **immutable, versioned data contracts** (DTOs).  
 No shared memory. No direct function calls across modules.

---

## **Why this matters**

Without DTO isolation:

* hidden coupling → unpredictable behavior  
* impossible to replay system  
* learning becomes invalid (non-reproducible)

---

## **DTO Design Rules (STRICT)**

### **1\. Immutable**

type EdgeDTO struct {  
   TokenAddress string  
   Score float64  
   Timestamp int64  
}

* once created → NEVER mutated  
* updates \= new DTO

---

### **2\. Self-contained**

Bad:

type EdgeDTO struct {  
   TokenAddress string  
}  
// requires external lookup → NOT allowed

Good:

type EdgeDTO struct {  
   TokenAddress string  
   Liquidity float64  
   Tax float64  
   Score float64  
}  
---

### **3\. Versioned**

type EdgeDTOv2 struct {  
   TokenAddress string  
   Score float64  
   Confidence float64  
   Version int  
}  
---

### **4\. Typed per stage**

Each stage has its own DTO:

DetectDTO → FilterDTO → ScoreDTO → SelectionDTO → ExecutionDTO  
---

## **DTO Flow (deterministic)**

Event → DTO → Stored → Consumed → New DTO → Stored  
---

## **Key Property**

Same input DTO → same output DTO

This is what enables **replay \+ learning correctness**

---

# **2.2 EVENT BUS (APPEND-ONLY)**

---

## **Definition**

Central system component:

ALL state transitions \= events written to storage

No overwrites. No updates. Only inserts.

---

## **Implementation (Postgres)**

Single table (simplified):

events (  
 id BIGSERIAL PRIMARY KEY,  
 event\_type TEXT,  
 payload JSONB,  
 created\_at TIMESTAMP,  
 processed BOOLEAN DEFAULT FALSE  
)  
---

## **Event Types**

market\_data\_event  
data\_quality\_event  
feature\_event  
edge\_event  
selection\_event  
execution\_event  
position\_event  
evaluation\_event  
adjustment\_event  
telegram\_event  
---

## **Flow**

Producer → INSERT event  
Worker → SELECT unprocessed event  
Worker → process → INSERT next event  
---

## **Why Append-Only?**

### **1\. Full audit trail**

You can reconstruct EVERYTHING:

why did we buy this token?  
---

### **2\. Replay capability**

Critical for learning:

re-run system with new parameters  
---

### **3\. Determinism**

No state mutation \= no hidden bugs

---

## **Anti-patterns (DO NOT DO)**

* updating rows  
* deleting events  
* mixing state \+ events

---

# **2.3 WORKER-BASED EXECUTION (SKIP LOCKED)**

---

## **Problem**

Multiple workers processing same events → race conditions

---

## **Solution**

Use:

SELECT \* FROM events  
WHERE processed \= FALSE  
FOR UPDATE SKIP LOCKED  
LIMIT 1;  
---

## **How it works**

* worker A locks row  
* worker B skips locked row  
* no duplicate processing

---

## **Worker Model**

Each stage \= independent worker group

DetectWorker  
FilterWorker  
ScoreWorker  
ExecutionWorker  
...  
---

## **Worker Loop (Go-style)**

for {  
   event := fetchUnprocessedEvent()

   if event \== nil {  
       sleep()  
       continue  
   }

   result := process(event)

   writeNewEvent(result)

   markProcessed(event)  
}  
---

## **Scaling**

You can scale horizontally:

1 → 10 → 100 workers

No logic changes required.

---

## **Backpressure Handling**

If queue grows:

slow downstream → backlog increases

Solutions:

* increase workers  
* drop low-priority events  
* throttle upstream

---

## **Determinism Guarantee**

Even with concurrency:

each event processed exactly once  
---

# **2.4 PER-MARKET ISOLATION**

---

## **Definition**

Each market \= isolated module

modules/  
 crypto\_dex/  
 crypto\_cex/  
 stocks/  
---

## **Why this matters**

Different markets:

* different data structures  
* different latency  
* different strategies

---

## **Isolation Rules**

### **1\. No cross-module calls**

Bad:

dex module calling cex logic  
---

### **2\. Only shared layer \= core**

core/  
 event\_bus  
 dto  
 learning  
 execution\_interface  
---

### **3\. Separate configs**

crypto\_dex:  
 min\_liquidity: 10k

crypto\_cex:  
 min\_volume: 1M  
---

## **Benefit**

You can:

* evolve DEX without breaking CEX  
* run experiments independently  
* isolate failures

---

## **Important Extension**

Also isolate:

strategy variants

Example:

dex\_sniper\_v1  
dex\_sniper\_v2  
---

# **2.5 TELEGRAM VIA EVENT BUS (NO DIRECT CALLS)**

---

## **Definition**

Telegram is NOT a controller.

It is:

event producer \+ event consumer  
---

## **Flow**

### **A. System → Telegram**

system emits → telegram\_event  
telegram worker → sends message  
---

### **B. Telegram → System**

user command → telegram\_command\_event  
worker processes → emits system event  
---

## **Example**

---

### **User sends:**

/stop  
---

### **Flow:**

Telegram API  
→ create telegram\_command\_event  
→ event bus  
→ control worker  
→ emits system\_control\_event  
→ execution engine stops  
---

## **Why this matters**

### **1\. No tight coupling**

Telegram failure ≠ system failure

---

### **2\. Full audit trail**

who stopped system?  
---

### **3\. Replayable control**

You can simulate:

what if /stop wasn’t sent?  
---

## **Command DTO**

type TelegramCommandDTO struct {  
   Command string  
   Params map\[string\]string  
   UserID string  
}  
---

## **Alert DTO**

type TelegramAlertDTO struct {  
   Type string  
   Message string  
   Severity string  
}  
---

## **Guardrails**

* rate limit messages  
* queue messages (don’t block system)  
* retry on failure

---

# **2.6 HOW ALL PARTS CONNECT (FULL FLOW)**

---

DEX Listener  
   ↓  
INSERT market\_event  
   ↓  
Filter Worker  
   ↓  
INSERT data\_quality\_event  
   ↓  
Score Worker  
   ↓  
INSERT edge\_event  
   ↓  
Selection Worker  
   ↓  
INSERT selection\_event  
   ↓  
Execution Worker  
   ↓  
INSERT execution\_event  
   ↓  
Position Worker  
   ↓  
INSERT position\_event  
   ↓  
Evaluation Worker  
   ↓  
INSERT evaluation\_event  
   ↓  
Learning Worker  
   ↓  
INSERT adjustment\_event  
---

# **2.7 NON-NEGOTIABLE PROPERTIES**

---

## **1\. Reproducibility**

same event stream → same result  
---

## **2\. Observability**

Everything is:

logged \+ queryable  
---

## **3\. Scalability**

Workers scale independently

---

## **4\. Fault Tolerance**

* worker crash ≠ system crash  
* events persist

---

# **2.8 WHAT WILL BREAK IF YOU VIOLATE THIS**

---

## **If no DTO isolation**

→ hidden coupling  
 → learning becomes invalid

---

## **If no append-only**

→ no replay  
 → no debugging

---

## **If no SKIP LOCKED**

→ duplicate trades  
 → capital loss

---

## **If Telegram bypasses system**

→ inconsistent state  
 → unreproducible behavior

---

# **FINAL INSIGHT**

This backbone turns your system into:

EVENT-SOURCED, DETERMINISTIC TRADING MACHINE

Not:

* script  
* bot  
* random async system

