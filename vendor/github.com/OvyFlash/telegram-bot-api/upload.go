package tgbotapi

import (
	"fmt"
	"io"
	"mime/multipart"
	"strings"
)

type requestPayload struct {
	body        io.Reader
	closer      io.Closer
	contentType string
}

func (p requestPayload) close() {
	if p.closer != nil {
		_ = p.closer.Close()
	}
}

type requestDebug struct {
	params    Params
	fileCount int
}

type uploadPlan struct {
	files  []RequestFile
	inline Params
}

func newUploadPlan() *uploadPlan {
	return &uploadPlan{}
}

func (p *uploadPlan) AddField(name string, data RequestFileData) {
	if data == nil {
		return
	}
	if data.NeedsUpload() {
		p.addUpload(name, data)
		return
	}
	p.addInline(name, data.SendData())
}

func (p *uploadPlan) AddUploadOnly(name string, data RequestFileData) {
	if data == nil || !data.NeedsUpload() {
		return
	}
	p.addUpload(name, data)
}

func (p *uploadPlan) Apply(params Params) Params {
	if len(p.inline) == 0 {
		return params
	}
	if params == nil {
		params = make(Params)
	}
	for key, value := range p.inline {
		params[key] = value
	}
	return params
}

func (p *uploadPlan) Files() []RequestFile {
	return p.files
}

func (p *uploadPlan) NeedsUpload() bool {
	return len(p.files) > 0
}

func (p *uploadPlan) addUpload(name string, data RequestFileData) {
	p.files = append(p.files, RequestFile{
		Name: name,
		Data: data,
	})
}

func (p *uploadPlan) addInline(name, value string) {
	if p.inline == nil {
		p.inline = make(Params)
	}
	p.inline[name] = value
}

func uploadPlanFromFiles(files []RequestFile) *uploadPlan {
	plan := newUploadPlan()
	for _, file := range files {
		plan.AddField(file.Name, file.Data)
	}
	return plan
}

func requestFiles(files ...RequestFile) []RequestFile {
	out := make([]RequestFile, 0, len(files))
	for _, file := range files {
		if file.Data != nil {
			out = append(out, file)
		}
	}
	return out
}

func requestFile(name string, data RequestFileData) RequestFile {
	return RequestFile{
		Name: name,
		Data: data,
	}
}

func buildFormPayload(params Params) requestPayload {
	values := buildParams(params)
	return requestPayload{
		body:        strings.NewReader(values.Encode()),
		contentType: "application/x-www-form-urlencoded",
	}
}

func buildMultipartPayload(params Params, files []RequestFile) (requestPayload, error) {
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)

	go func() {
		if err := writeMultipartPayload(multipartWriter, params, files); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		if err := multipartWriter.Close(); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		_ = writer.Close()
	}()

	return requestPayload{
		body:        reader,
		closer:      reader,
		contentType: multipartWriter.FormDataContentType(),
	}, nil
}

func writeMultipartPayload(writer *multipart.Writer, params Params, files []RequestFile) error {
	for field, value := range params {
		if err := writer.WriteField(field, value); err != nil {
			return fmt.Errorf("write multipart field %q: %w", field, err)
		}
	}

	for _, file := range files {
		if file.Data == nil {
			return fmt.Errorf("multipart file %q has nil data", file.Name)
		}

		if file.Data.NeedsUpload() {
			if err := writeMultipartUpload(writer, file); err != nil {
				return err
			}
			continue
		}

		if err := writer.WriteField(file.Name, file.Data.SendData()); err != nil {
			return fmt.Errorf("write multipart file reference %q: %w", file.Name, err)
		}
	}

	return nil
}

func writeMultipartUpload(writer *multipart.Writer, file RequestFile) error {
	name, reader, err := file.Data.UploadData()
	if err != nil {
		return fmt.Errorf("open upload %q: %w", file.Name, err)
	}
	if reader == nil {
		return fmt.Errorf("open upload %q: nil reader", file.Name)
	}

	part, err := writer.CreateFormFile(file.Name, name)
	if err != nil {
		if closeErr := closeUploadReader(reader); closeErr != nil {
			return fmt.Errorf("create multipart file %q: %w; close upload: %w", file.Name, err, closeErr)
		}
		return fmt.Errorf("create multipart file %q: %w", file.Name, err)
	}

	_, copyErr := io.Copy(part, reader)
	closeErr := closeUploadReader(reader)
	if copyErr != nil {
		if closeErr != nil {
			return fmt.Errorf("copy upload %q: %w; close upload: %w", file.Name, copyErr, closeErr)
		}
		return fmt.Errorf("copy upload %q: %w", file.Name, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close upload %q: %w", file.Name, closeErr)
	}

	return nil
}

func closeUploadReader(reader io.Reader) error {
	if closer, ok := reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func prepareInputMediaUploadPlan(inputMedia []InputMedia, prefix string) ([]InputMedia, *uploadPlan) {
	prepared := cloneMediaSlice(inputMedia)
	plan := newUploadPlan()

	for idx, media := range prepared {
		if media == nil {
			continue
		}

		name := fmt.Sprintf("%s-%d", prefix, idx)
		prepareInputMediaItem(media, name, plan)
		prepared[idx] = media
	}

	return prepared, plan
}

func prepareInputRichMessageUploadPlan(message InputRichMessage) (InputRichMessage, *uploadPlan) {
	prepared := message
	prepared.Media = append([]InputRichMessageMedia(nil), message.Media...)
	plan := newUploadPlan()

	for idx := range prepared.Media {
		media := cloneInputMedia(prepared.Media[idx].Media)
		if media == nil {
			continue
		}

		prepareInputMediaItem(media, fmt.Sprintf("rich-message-media-%d", idx), plan)
		prepared.Media[idx].Media = media
	}

	prepared.Blocks = prepareInputRichBlocks(message.Blocks, "rich-message-block", plan)

	return prepared, plan
}

func prepareInputRichBlocks(blocks []InputRichBlock, prefix string, plan *uploadPlan) []InputRichBlock {
	if blocks == nil {
		return nil
	}

	prepared := make([]InputRichBlock, len(blocks))
	for idx, block := range blocks {
		prepared[idx] = prepareInputRichBlock(block, fmt.Sprintf("%s-%d", prefix, idx), plan)
	}

	return prepared
}

func prepareInputRichBlock(block InputRichBlock, name string, plan *uploadPlan) InputRichBlock {
	switch current := block.(type) {
	case InputRichBlockList:
		return prepareInputRichBlockList(current, name, plan)
	case *InputRichBlockList:
		if current == nil {
			return current
		}
		prepared := prepareInputRichBlockList(*current, name, plan)
		return &prepared
	case InputRichBlockBlockQuotation:
		current.Blocks = prepareInputRichBlocks(current.Blocks, name+"-block", plan)
		return current
	case *InputRichBlockBlockQuotation:
		if current == nil {
			return current
		}
		prepared := *current
		prepared.Blocks = prepareInputRichBlocks(current.Blocks, name+"-block", plan)
		return &prepared
	case InputRichBlockCollage:
		current.Blocks = prepareInputRichBlocks(current.Blocks, name+"-block", plan)
		return current
	case *InputRichBlockCollage:
		if current == nil {
			return current
		}
		prepared := *current
		prepared.Blocks = prepareInputRichBlocks(current.Blocks, name+"-block", plan)
		return &prepared
	case InputRichBlockSlideshow:
		current.Blocks = prepareInputRichBlocks(current.Blocks, name+"-block", plan)
		return current
	case *InputRichBlockSlideshow:
		if current == nil {
			return current
		}
		prepared := *current
		prepared.Blocks = prepareInputRichBlocks(current.Blocks, name+"-block", plan)
		return &prepared
	case InputRichBlockDetails:
		current.Blocks = prepareInputRichBlocks(current.Blocks, name+"-block", plan)
		return current
	case *InputRichBlockDetails:
		if current == nil {
			return current
		}
		prepared := *current
		prepared.Blocks = prepareInputRichBlocks(current.Blocks, name+"-block", plan)
		return &prepared
	case InputRichBlockAnimation:
		prepareInputMediaItem(&current.Animation, name, plan)
		return current
	case *InputRichBlockAnimation:
		if current == nil {
			return current
		}
		prepared := *current
		prepareInputMediaItem(&prepared.Animation, name, plan)
		return &prepared
	case InputRichBlockAudio:
		prepareInputMediaItem(&current.Audio, name, plan)
		return current
	case *InputRichBlockAudio:
		if current == nil {
			return current
		}
		prepared := *current
		prepareInputMediaItem(&prepared.Audio, name, plan)
		return &prepared
	case InputRichBlockPhoto:
		prepareInputMediaItem(&current.Photo, name, plan)
		return current
	case *InputRichBlockPhoto:
		if current == nil {
			return current
		}
		prepared := *current
		prepareInputMediaItem(&prepared.Photo, name, plan)
		return &prepared
	case InputRichBlockVideo:
		prepareInputMediaItem(&current.Video, name, plan)
		return current
	case *InputRichBlockVideo:
		if current == nil {
			return current
		}
		prepared := *current
		prepareInputMediaItem(&prepared.Video, name, plan)
		return &prepared
	case InputRichBlockVoiceNote:
		prepareInputMediaItem(&current.VoiceNote, name, plan)
		return current
	case *InputRichBlockVoiceNote:
		if current == nil {
			return current
		}
		prepared := *current
		prepareInputMediaItem(&prepared.VoiceNote, name, plan)
		return &prepared
	default:
		return block
	}
}

func prepareInputRichBlockList(block InputRichBlockList, name string, plan *uploadPlan) InputRichBlockList {
	block.Items = append([]InputRichBlockListItem(nil), block.Items...)
	for itemIdx := range block.Items {
		prefix := fmt.Sprintf("%s-item-%d-block", name, itemIdx)
		block.Items[itemIdx].Blocks = prepareInputRichBlocks(block.Items[itemIdx].Blocks, prefix, plan)
	}

	return block
}

func prepareInputMediaItem(media InputMedia, name string, plan *uploadPlan) {
	if file := media.getMedia(); file != nil && file.NeedsUpload() {
		media.setUploadMedia("attach://" + name)
		plan.AddUploadOnly(name, file)
	}

	switch input := media.(type) {
	case *InputMediaLivePhoto:
		prepareInputMediaSecondary(input.Photo, name+"-photo", plan, func(ref string) {
			input.Photo = fileAttach(ref)
		})
	case *InputPaidMedia:
		prepareInputPaidMediaPhoto(input, name, plan)
	}

	if thumb := media.getThumb(); thumb != nil && thumb.NeedsUpload() {
		media.setUploadThumb("attach://" + name + "-thumb")
		plan.AddUploadOnly(name+"-thumb", thumb)
	}
}

func prepareInputPaidMediaPhoto(media *InputPaidMedia, name string, plan *uploadPlan) {
	if livePhoto, ok := media.Media.(*InputMediaLivePhoto); ok && livePhoto.Photo != nil {
		prepareInputMediaSecondary(livePhoto.Photo, name+"-photo", plan, func(ref string) {
			livePhoto.Photo = fileAttach(ref)
		})
		return
	}

	prepareInputMediaSecondary(media.Photo, name+"-photo", plan, func(ref string) {
		media.Photo = fileAttach(ref)
	})
}

func prepareInputMediaSecondary(data RequestFileData, name string, plan *uploadPlan, set func(string)) {
	if data == nil || !data.NeedsUpload() {
		return
	}
	set("attach://" + name)
	plan.AddUploadOnly(name, data)
}

func prepareInputProfilePhotoUploadPlan(photo InputProfilePhoto) (InputProfilePhoto, *uploadPlan) {
	prepared := cloneInputProfilePhoto(photo)
	plan := newUploadPlan()
	if prepared == nil {
		return nil, plan
	}

	if media := prepared.getMedia(); media != nil && media.NeedsUpload() {
		prepared.setUploadMedia("attach://file-0")
		plan.AddUploadOnly("file-0", media)
	}

	return prepared, plan
}

func prepareInputStoryContentUploadPlan(content InputStoryContent) (InputStoryContent, *uploadPlan) {
	prepared := cloneInputStoryContent(content)
	plan := newUploadPlan()
	if prepared == nil {
		return nil, plan
	}

	if media := prepared.getMedia(); media != nil && media.NeedsUpload() {
		prepared.setUploadMedia("attach://file-0")
		plan.AddUploadOnly("file-0", media)
	}

	return prepared, plan
}
