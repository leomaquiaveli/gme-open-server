# scroll_x — Guia de Posicionamento Horizontal

Este documento explica como o parâmetro `scroll_x` funciona geometricamente e como um modelo de visão (ex: Gemini) pode calcular o valor correto a partir da posição do sujeito no frame original.

---

## 1. O que o scroll_x faz

A rota `/v1/media/vertical` recorta uma janela vertical 9:16 de dentro de um frame horizontal 16:9. O `scroll_x` controla onde essa janela está posicionada horizontalmente:

```
Frame 16:9 (ex: 1280×720)
┌──────────────────────────────────────────────────────────┐
│                                                          │
│   scroll_x = -100          scroll_x = 0                 │
│   ┌─────────┐              ┌─────────┐                   │
│   │         │              │         │                   │
│   │  janela │              │  janela │       ...         │
│   │  9:16   │              │  9:16   │                   │
│   └─────────┘              └─────────┘                   │
│   ↑ borda esquerda         ↑ centro                      │
│                                                          │
└──────────────────────────────────────────────────────────┘

         scroll_x = +100
         ┌──────────────────────────────┐─────────┐
         │                              │  janela │
         │                              │  9:16   │
         │                              └─────────┘
                                        ↑ borda direita
```

**Valores:**
- `-100` → janela na borda esquerda do frame
- `0` → janela no centro (default)
- `+100` → janela na borda direita do frame

---

## 2. Geometria interna (como o FFmpeg calcula)

Para qualquer resolução de entrada, o filtro é:

```
crop = (ih × 9/16) : ih : (iw − ih×9/16)/2 × (1 + scroll_x/100) : 0
```

| Variável | Significado |
|---|---|
| `ih` | altura do frame de entrada |
| `iw` | largura do frame de entrada |
| `ih × 9/16` | largura da janela 9:16 |
| `(iw − janela_w)/2` | margem disponível de cada lado |
| `× (1 + scroll_x/100)` | fator de deslocamento |

**Exemplos por resolução:**

| Resolução | Largura da janela | Margem total | x em scroll=-100 | x em scroll=0 | x em scroll=+100 |
|---|---|---|---|---|---|
| 1280×720 | 405px | 875px | 0px | 437px | 875px |
| 1920×1080 | 607px | 1313px | 0px | 656px | 1313px |

O filtro usa `iw` e `ih` em runtime — funciona para qualquer resolução de entrada.

---

## 3. Mapeamento: posição do sujeito → scroll_x

Se o sujeito principal está na posição horizontal `X` do frame (0=esquerda, 0.5=centro, 1=direita), o `scroll_x` ideal para centralizá-lo na janela vertical é:

```
scroll_x = (512 × X − 256) / 1.75
```

Resultado clampado em [-100, 100].

**Tabela de referência:**

| Posição do sujeito no frame | X normalizado | scroll_x ideal |
|---|---|---|
| Borda esquerda (0–10%) | 0.05 | -100 |
| Quarto esquerdo (15–25%) | 0.20 | -73 |
| Terço esquerdo (28–38%) | 0.33 | -50 |
| Levemente à esquerda (38–47%) | 0.42 | -29 |
| Centro (47–53%) | 0.50 | 0 |
| Levemente à direita (53–62%) | 0.58 | +29 |
| Terço direito (62–72%) | 0.67 | +50 |
| Quarto direito (75–85%) | 0.80 | +73 |
| Borda direita (90–100%) | 0.95 | +100 |

---

## 4. Guia para o modelo de visão (Gemini)

### Prompt base sugerido

```
Analise o frame do vídeo e identifique a posição horizontal do sujeito principal (pessoa, objeto ou área de interesse).

Retorne um JSON com:
{
  "subject_x": <float 0.0 a 1.0>,  // posição horizontal normalizada: 0=esquerda, 1=direita
  "scroll_x": <int -100 a 100>,    // valor calculado para scroll_x
  "justification": "<texto>"       // onde está o sujeito e por que esse valor
}

Regras de cálculo:
  scroll_x = round((512 × subject_x − 256) / 1.75)
  Clampe o resultado em [-100, 100].

Exemplos:
  - Pessoa no centro do frame → subject_x=0.5 → scroll_x=0
  - Pessoa no terço esquerdo → subject_x=0.33 → scroll_x=-50
  - Pessoa no quarto direito → subject_x=0.75 → scroll_x=+73
```

### Considerações adicionais para o prompt

**Regra dos terços:** para conteúdo dinâmico (entrevistas, ação), evitar scroll_x=0 exato pode ficar mais natural. Se o sujeito está olhando para a direita, desloque levemente para a esquerda (+10 a +20) para dar "espaço de respiração" na direção do olhar.

**Sujeito saindo do frame:** se o sujeito está muito próximo da borda e parte do corpo ficaria cortada com o scroll_x calculado, ajuste em direção ao centro em ~20 pontos.

**Múltiplos sujeitos:** use a média ponderada das posições (dê peso 1.5 para o sujeito que fala ou está em movimento).

**Frame sem sujeito claro:** use scroll_x=0 (centro).

---

## 5. Exemplos práticos

### Entrevista — sujeito levemente à esquerda

```
Frame 16:9:  [  SUJEITO  ][              ]
              ~33% da largura

scroll_x = (512 × 0.33 − 256) / 1.75 = −50
```

Resultado: a janela 9:16 captura o sujeito centralizado.

### Dois sujeitos — um de cada lado

```
Frame 16:9:  [ SUJEITO A ][ espaço ][ SUJEITO B ]
              ~20%                     ~80%

Média: X = (0.20 + 0.80) / 2 = 0.50
scroll_x = 0  → captura o centro, inclui os dois
```

### Ação — pessoa correndo para a direita

```
Frame 16:9:  [       ][ CORREDOR→ ]
              sujeito em ~65%

scroll_x = (512 × 0.65 − 256) / 1.75 = +60
Ajuste "espaço de direção": +60 − 15 = +45
```

---

## 6. Integração com o workflow

No N8N ou qualquer orquestrador:

1. **Screenshot** do frame representativo do clipe (1s de duração, sem encode)
2. **Gemini Vision** analisa o frame → retorna `scroll_x`
3. **POST `/v1/media/vertical`** com o `scroll_x` calculado

```json
{
  "input_url": "https://storage.googleapis.com/bucket/video.mp4",
  "file_name": "clip-vertical.mp4",
  "scroll_x": -50,
  "start": 15.0,
  "duration": 30.0,
  "crf": 23,
  "preset": "fast"
}
```
