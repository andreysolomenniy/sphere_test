package main
// curl -X POST -F file=@/home/solo/work/go/src/github.com/andreysolomenniy/sphere_test/main.go http://localhost:4000/api/uploadVideo
// curl -X POST -F file=@/home/solo/da.mp4 http://localhost:4000/api/uploadVideo
// curl -X POST -F file=@/home/solo/dtears-of_steel.mp4 http://localhost:4000/api/uploadVideo

import (
    "errors"
    "strings"
    "mime"
    "os"
	"io"
	"io/ioutil"
	"log"
	"net/http"
    "encoding/json"
    "github.com/alfg/mp4/atom"
)

func main() {
	http.HandleFunc("/api/uploadVideo", uploadRouterHandler)
	err := http.ListenAndServe(":4000", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

// роутер
func uploadRouterHandler(w http.ResponseWriter, r *http.Request) {
    // Выбираем файл из запроса если он там есть и записываем его во временную папку
    // В случае любой ошибки отправляем 400
    tmpFile, err := getFileFromMultipart(r)
    if tmpFile != nil {
        defer os.Remove(tmpFile.Name())
        defer tmpFile.Close()
    }
    if err != nil {
		log.Println("Error:", err)
		w.WriteHeader(400)
		return
    }
    
    // Создаём объект метаданных MPEG-4 и парсим в него метаданные из временного файла
    f := &atom.File{
		File: tmpFile,
	}
	err = f.Parse()
    if err != nil {
		log.Println("Error:", err)
		w.WriteHeader(400)
		return
    }
    
    // Создаём json из метаданных
    json, err := makeJsonFromMetadata(f)
    
    // Если всё Ок, то отдаём этот json
    w.WriteHeader(500)
    w.Write(json)  
}

// Выбираем файл из запроса если он там есть и записываем его во временную папку.
// На входе запроса.
// На выходе файл и ошибка.
// Внимание! Файл не закрывается и не удаляется из папки.
func getFileFromMultipart(r *http.Request) (*os.File, error) {
    // Считываем тип контента и проверяем мультипарт ли это
    mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
    if err != nil {
		return nil, err
    }
    if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, errors.New("Not multipart.")
    }
    // Создаём ридера мультипарта и считываем первую часть - файл из запроса.
    // (файл предполагается только один)
    mr, err := r.MultipartReader()
    if err != nil {
		return nil, err
    }
	p, err := mr.NextPart()
	if err == io.EOF {
		return nil, errors.New("No file inside.")
	}
	if err != nil {
		return nil, err
	}
	// Создаём временный файл в системной папке для временных файлов.
	tmpFile, err := ioutil.TempFile("", "mediaGotFromRequest")
	if err != nil {
		return nil, err
	}
	// Пишем в него файл из запроса
	_, err = io.Copy(tmpFile, p)
	if err != nil {
		return nil, err
	}

	// Возвращаем файл и признак успешности.
	return tmpFile, nil
}

// Структура для аудио-дорожки
type metaDataAudio struct {
    Size int64
    Duration uint32
    Language string
    TimeScale uint32
}

// Структура для видео-дорожки
type metaDataVideo struct {
    Size int64
    Duration uint32
    Language string
    TimeScale uint32
    Width uint32
    Height uint32
}

// Структура для метаданных.
// Включает в себя ссылки на аудио и видео.
type metaData struct {
    MajorBrand string
    MinorVersion uint32
    CompatibleBrands []string
    Size int64
    Audio *metaDataAudio
    Video *metaDataVideo
}

// Создаём json из метаданных
// На входе файл
// На выходе json и ошибка
func makeJsonFromMetadata(f *atom.File) ([]byte, error) {
    var md metaData
    // Поскольку метаданные в библиотеке реализованы в виде дерева
    // со структурами ссылающимися друг на друга, и они создаются 
    // только при наличии данных в файле, необходимо перед каждым 
    // использованием полей проверять указатель на nil
    if f.Ftyp != nil {
        md.MajorBrand = f.Ftyp.MajorBrand 
        md.MinorVersion = f.Ftyp.MinorVersion
        md.CompatibleBrands = f.Ftyp.CompatibleBrands
    }
    if f.Moov != nil {
        md.Size = f.Moov.Size 
    }
    // выбираем один трек аудио и один трек видео
    for _, t := range f.Moov.Traks {
        if t.Mdia != nil && t.Mdia.Hdlr != nil {
            if t.Mdia.Hdlr.Handler == "soun" { // хардкорный признак аудео
                mda := &metaDataAudio {}
                md.Audio = mda
                mda.Size = t.Size
                if t.Tkhd != nil {
                    mda.Duration = t.Tkhd.Duration
                }
                if t.Mdia.Mdhd != nil {
                    mda.Language = t.Mdia.Mdhd.LanguageString
                    mda.TimeScale = t.Mdia.Mdhd.Timescale
                }
            }
            if t.Mdia.Hdlr.Handler == "vide" { // хардкорный признак видео
                mdv := &metaDataVideo {}
                md.Video = mdv
                mdv.Size = t.Size
                if t.Tkhd != nil {
                    mdv.Duration = t.Tkhd.Duration
                }
                if t.Mdia.Mdhd != nil {
                    mdv.Language = t.Mdia.Mdhd.LanguageString
                    mdv.TimeScale = t.Mdia.Mdhd.Timescale
                }
                if f.Moov != nil && f.Moov.Mvhd != nil && t.Tkhd != nil {
                    // Чтобы вычислить размеры окна, делим размеры на рэйт
                    mdv.Width = uint32(t.Tkhd.Width / f.Moov.Mvhd.Rate)
                    mdv.Height = uint32(t.Tkhd.Height / f.Moov.Mvhd.Rate)
                }
            }
        }
    }
    // По заполнению структуры преобразуем данные из неё в json.
    b, err := json.MarshalIndent(md,"","\t")
    if err != nil {
        return nil, err
    }
    return b, nil
}
