Do not introduce any of these unless the project explicitly requires them:

| Category     | Default Forbidden                                                                                            | Override             |
| ------------ | ------------------------------------------------------------------------------------------------------------ | -------------------- |
| Architecture | Microservices, Kafka, RabbitMQ, Kubernetes, Docker orchestration                                             | Unless project needs |
| Databases    | MongoDB, Redis, any distributed database                                                                     | Unless project needs |
| AI/ML        | OpenAI API, Anthropic API, LangChain, AutoGPT, CrewAI, any paid LLM                                          | Unless project needs |
| Cloud        | AWS, GCP, Azure, any cloud compute or storage                                                                | Unless project needs |
| Runtime      | Agent loops, autonomous planners, async message brokers (Kafka/RabbitMQ/NATS) outside the Postgres event bus | Unless project needs |

> **Override policy:** If your project legitimately requires a forbidden technology (e.g., Redis for caching, Docker for deployment, OpenAI for an LLM-powered feature), document the justification in `docs/architecture.md` and proceed. The defaults exist to prevent accidental complexity, not to block valid requirements.