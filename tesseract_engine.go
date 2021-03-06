package ocrworker

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/couchbaselabs/logg"
)

// This variant of the TesseractEngine calls tesseract via exec
type TesseractEngine struct {
}

type TesseractEngineArgs struct {
	configVars  map[string]string `json:"config_vars"`
	pageSegMode string            `json:"psm"`
	lang        string            `json:"lang"`
}

func NewTesseractEngineArgs(ocrRequest OcrRequest) (*TesseractEngineArgs, error) {

	engineArgs := &TesseractEngineArgs{}

	if ocrRequest.EngineArgs == nil {
		return engineArgs, nil
	}

	// config vars
	configVarsMapInterfaceOrig := ocrRequest.EngineArgs["config_vars"]

	if configVarsMapInterfaceOrig != nil {

		logg.LogTo("OCR_TESSERACT", "got configVarsMap: %v type: %T", configVarsMapInterfaceOrig, configVarsMapInterfaceOrig)

		configVarsMapInterface := configVarsMapInterfaceOrig.(map[string]interface{})

		configVarsMap := make(map[string]string)
		for k, v := range configVarsMapInterface {
			v, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("Could not convert configVar into string: %v", v)
			}
			configVarsMap[k] = v
		}

		engineArgs.configVars = configVarsMap

	}

	// page seg mode
	pageSegMode := ocrRequest.EngineArgs["psm"]
	if pageSegMode != nil {
		pageSegModeStr, ok := pageSegMode.(string)
		if !ok {
			return nil, fmt.Errorf("Could not convert psm into string: %v", pageSegMode)
		}
		engineArgs.pageSegMode = pageSegModeStr
	}

	// language
	lang := ocrRequest.EngineArgs["lang"]
	if lang != nil {
		langStr, ok := lang.(string)
		if !ok {
			return nil, fmt.Errorf("Could not convert lang into string: %v", lang)
		}
		engineArgs.lang = langStr
	}

	return engineArgs, nil

}

// return a slice that can be passed to tesseract binary as command line
// args, eg, ["-c", "tessedit_char_whitelist=0123456789", "-c", "foo=bar"]
func (t TesseractEngineArgs) Export() []string {
	result := []string{}
	for k, v := range t.configVars {
		result = append(result, "-c")
		keyValArg := fmt.Sprintf("%s=%s", k, v)
		result = append(result, keyValArg)
	}
	if t.pageSegMode != "" {
		result = append(result, "-psm")
		result = append(result, t.pageSegMode)
	}
	if t.lang != "" {
		result = append(result, "-l")
		result = append(result, t.lang)
	}

	return result
}

func (t TesseractEngine) ProcessRequest(ocrRequest OcrRequest) (OcrResult, error) {

	tmpFileName, err := func() (string, error) {
		if ocrRequest.ImgUrl != "" {
			return t.tmpFileFromImageUrl(ocrRequest.ImgUrl)
		} else {
			return t.tmpFileFromImageBytes(ocrRequest.ImgBytes, ocrRequest.Name)
		}

	}()

	if err != nil {
		logg.LogTo("OCR_TESSERACT", "error getting tmpFileName")
		return OcrResult{}, err
	}

	defer os.Remove(tmpFileName)

	engineArgs, err := NewTesseractEngineArgs(ocrRequest)
	if err != nil {
		logg.LogTo("OCR_TESSERACT", "error getting engineArgs")
		return OcrResult{}, err
	}

	ocrResult, err := t.processImageFile(tmpFileName, *engineArgs)

	return ocrResult, err

}

func (t TesseractEngine) tmpFileFromImageBytes(imgBytes []byte, name string) (string, error) {

	logg.LogTo("TEST1", "%s", name)
	tmpFileName := func(url string) string {
		baseName := filepath.Base(url)
		extension := filepath.Ext(baseName)
		baseNameNoExt := url[0:len(baseName)-len(extension)]
		return filepath.Join(os.TempDir(), baseNameNoExt)
	}(name)
	logg.LogTo("TEST2", "%s", tmpFileName)

	// we have to write the contents of the image url to a temp
	// file, because the leptonica lib can't seem to handle byte arrays
	err := saveBytesToFileName(imgBytes, tmpFileName)
	if err != nil {
		return "", err
	}

	return tmpFileName, nil

}

func (t TesseractEngine) tmpFileFromImageUrl(imgUrl string) (string, error) {

	tmpFileName := func(url string) string {
		baseName := filepath.Base(url)
		extension := filepath.Ext(baseName)
		baseNameNoExt := url[0:len(baseName)-len(extension)]
		return filepath.Join(os.TempDir(), baseNameNoExt)
	}(imgUrl)
	// we have to write the contents of the image url to a temp
	// file, because the leptonica lib can't seem to handle byte arrays
	err := saveUrlContentToFileName(imgUrl, tmpFileName)
	if err != nil {
		return "", err
	}

	return tmpFileName, nil

}

func (t TesseractEngine) processImageFile(inputFilename string, engineArgs TesseractEngineArgs) (OcrResult, error) {

	// if the input filename is /tmp/ocrimage, set the output file basename
	// to /tmp/ocrimage as well, which will produce /tmp/ocrimage.txt output
	tmpOutFileBaseName := inputFilename

	// possible file extensions
	fileExtensions := []string{"txt", "hocr", "pdf"}

	// build args array
	cflags := engineArgs.Export()
	cmdArgs := []string{inputFilename, tmpOutFileBaseName, "pdf"}
	cmdArgs = append(cmdArgs, cflags...)
	logg.LogTo("OCR_TESSERACT", "cmdArgs: %v", cmdArgs)

	// exec tesseract
	cmd := exec.Command("tesseract", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logg.LogTo("OCR_TESSERACT", "Error exec tesseract: %v %v", err, string(output))
		return OcrResult{}, err
	}

	outBytes, outFile, err := findAndReadOutfile(tmpOutFileBaseName, fileExtensions)

	// delete output file when we are done
	defer os.Remove(outFile)

	if err != nil {
		logg.LogTo("OCR_TESSERACT", "Error getting data from out file: %v", err)
		return OcrResult{}, err
	}

	return OcrResult{
		Text: string(outBytes),
		BaseFileName: fmt.Sprintf("%v_ocrd", tmpOutFileBaseName),
	}, nil

}

func findOutfile(outfileBaseName string, fileExtensions []string) (string, error) {

	for _, fileExtension := range fileExtensions {

		outFile := fmt.Sprintf("%v.%v", outfileBaseName, fileExtension)
		logg.LogTo("OCR_TESSERACT", "checking if exists: %v", outFile)

		if _, err := os.Stat(outFile); err == nil {
			return outFile, nil
		}

	}

	return "", fmt.Errorf("Could not find outfile.  Basename: %v Extensions: %v", outfileBaseName, fileExtensions)

}

func findAndReadOutfile(outfileBaseName string, fileExtensions []string) ([]byte, string, error) {

	outfile, err := findOutfile(outfileBaseName, fileExtensions)
	if err != nil {
		return nil, "", err
	}
	outBytes, err := ioutil.ReadFile(outfile)
	if err != nil {
		return nil, "", err
	}
	return outBytes, outfile, nil

}
