package pkg

import (
	"fmt"
	"fusionn/config"
	"fusionn/logger"
	"os"
	"os/exec"
)

type SubTrans interface {
	Translate(path string, targetLang string) error
}

type subTrans struct {
}

func NewSubTrans() *subTrans {
	return &subTrans{}
}

func (s *subTrans) Translate(path string, targetLang string) error {
	llmSubTransPath, err := exec.LookPath("llm-subtrans")
	if err != nil {
		return fmt.Errorf("llm-subtrans not found: %w", err)
	}

	cmd := exec.Command(llmSubTransPath,
		path, // input file
		"-s", config.C.GetString(config.LLM_BASE),
		"-e", config.C.GetString(config.LLM_ENDPOINT),
		"-k", config.C.GetString(config.LLM_API_KEY),
		"-m", config.C.GetString(config.LLM_MODEL),
		"-l", targetLang,
		"--chat",
		"--systemmessages",
	)

	logger.S.Debugf("llm-subtrans: %s", cmd.String())

	// Create a pipe for the command's stdout
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Read and print output in real-time
	buf := make([]byte, 1024)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			logger.S.Infof("llm-subtrans: %s", string(buf[:n]))
		}
		if err != nil {
			break
		}
	}

	return cmd.Wait()
}
