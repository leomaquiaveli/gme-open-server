package ports

// IWebhookSender é a porta (Port) na Clean Architecture responsável por abstrair a comunicação 
// de saída para sistemas externos via callbacks.
// A abstração desvincula o término de um caso de uso das minúcias de rede (HTTP, retries). 
// O Domínio apenas avisa que terminou o trabalho através desta interface.
// Usada primariamente para enviar o resultado do job para o orquestrador (render engine TypeScript),
// mantendo compatibilidade com o padrão de webhook do GME Server Python Legado.
type IWebhookSender interface {
	Send(webhookURL string, payload any) error
}
