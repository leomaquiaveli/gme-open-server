package ports

// IStorage é a porta de saída (Driven Port) na Clean Architecture que abstrai o armazenamento
// persistente de arquivos.
// Esta interface existe para garantir que o núcleo de negócio não saiba e nem dependa de 
// onde os arquivos vivem fisicamente (GCS, S3, Azure ou disco local).
// Graças a este contrato, a troca do provedor de nuvem ocorre sem afetar nenhuma outra camada, 
// respeitando estritamente o Princípio da Inversão de Dependência (DIP).
type IStorage interface {
	Upload(localPath string, contentType string) (publicURL string, err error)
	Download(url string, destPath string) error
}
