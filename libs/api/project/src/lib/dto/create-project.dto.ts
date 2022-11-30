import { ApiProperty } from '@nestjs/swagger';

export class CreateProjectDto {
    @ApiProperty({ type: String })
    readonly title!: string;

    @ApiProperty({ type: String })
    readonly description!: string;

    @ApiProperty({ type: String })
    readonly thumbnail!: string;

    @ApiProperty({ type: [String] })
    readonly batchDownloadLinks!: string[];

    @ApiProperty({ type: String })
    readonly dateTimeCreated!: string;
}
