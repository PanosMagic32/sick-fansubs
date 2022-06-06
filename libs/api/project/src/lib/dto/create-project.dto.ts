export class CreateProjectDto {
  readonly title!: string;
  readonly description!: string;
  readonly thumbnail!: string;
  readonly batchDownloadLinks!: string[];
  readonly dateTimeCreated!: string;
}
