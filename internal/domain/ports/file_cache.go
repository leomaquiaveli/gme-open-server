package ports

// IFileCache é a porta (Port) na Clean Architecture responsável por abstrair o mecanismo
// de cache local de arquivos. 
// O objetivo desta abstração é proteger a camada de Domínio dos detalhes de implementação
// do cache (ex: mapas estáticos em memória, banco local).
// Ela evita downloads repetidos do mesmo vídeo fonte durante processamentos massivos.
// Para 1000 clips do mesmo arquivo de 1GB, por exemplo, ocorre apenas 1 download em vez de 1000.
type IFileCache interface {
	Get(url string) (localPath string, found bool)
	Put(url string, localPath string)
	Acquire(url string)    // incrementa refcount — arquivo em uso por um job
	Release(url string)    // decrementa — deleta do disco se TTL expirou e refcount=0
	Invalidate(url string) // remove entrada do cache sem tocar o disco
}
